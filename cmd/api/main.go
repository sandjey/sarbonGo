package main

//test 2

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"sarbonNew/internal/chat"
	"sarbonNew/internal/calls"
	"sarbonNew/internal/config"
	"sarbonNew/internal/infra"
	"sarbonNew/internal/logger"
	"sarbonNew/internal/server"
	"sarbonNew/migrations"
)

func main() {
	config.LoadDotEnvUp(8)

	// Ring buffer for the /terminal live view. Every log line written by this
	// logger is mirrored here (with sensitive values masked) so the browser
	// can display the same stream the server sees on stdout.
	logHub := logger.NewLogHub(2000)

	var log *zap.Logger
	if os.Getenv("APP_ENV") == "local" {
		log = logger.NewDevelopmentWithHub(logHub)
	} else {
		var err error
		log, err = logger.NewProductionWithHub(logHub)
		if err != nil {
			panic(err)
		}
	}
	defer func() { _ = log.Sync() }()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal("config load failed", zap.Error(err))
	}
	log.Info("otp config",
		zap.Bool("telegram_gateway_bypass", cfg.TelegramGatewayBypass),
		zap.Int("otp_len", cfg.OTPLength),
		zap.Duration("otp_ttl", cfg.OTPTTL),
		zap.Duration("otp_resend_cooldown", cfg.OTPResendCooldown),
		zap.Int("otp_send_limit_phone_per_hour", cfg.OTPSendLimitPerPhonePerHour),
		zap.Int("otp_send_limit_ip_per_hour", cfg.OTPSendLimitPerIPPerHour),
		zap.Duration("otp_send_window", cfg.OTPSendWindow),
	)

	// Авто-миграции при старте API (встроенные SQL из пакета migrations — работает из любой cwd).
	// Отключить: SKIP_MIGRATIONS=1
	if strings.TrimSpace(os.Getenv("SKIP_MIGRATIONS")) != "1" {
		if err := runMigrationsUp(log, cfg.DatabaseURL); err != nil {
			log.Fatal("migrations up failed", zap.Error(err))
		}
	} else {
		log.Warn("SKIP_MIGRATIONS=1 — миграции пропущены")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	infraDeps, err := infra.New(ctx, cfg, log)
	if err != nil {
		log.Fatal("infra init failed", zap.Error(err))
	}
	defer infraDeps.Close()

	startChatMediaGC(ctx, log, infraDeps)
	startCallsSweeper(ctx, log, infraDeps, cfg)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.NewRouter(cfg, infraDeps, log, logHub),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http server starting", zap.String("addr", cfg.HTTPAddr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http server error", zap.Error(err))
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func startCallsSweeper(ctx context.Context, log *zap.Logger, deps *infra.Infra, cfg config.Config) {
	// Enabled by default in non-local too; keep it always on.
	repo := calls.NewRepo(deps.PG)
	timeout := cfg.CallsRingingTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := repo.MarkMissedExpired(ctx, timeout, 200)
				if err != nil {
					log.Warn("calls sweeper failed", zap.Error(err))
					continue
				}
				if n > 0 {
					log.Info("calls sweeper: missed", zap.Int64("count", n))
				}
			}
		}
	}()
}

func startChatMediaGC(ctx context.Context, log *zap.Logger, deps *infra.Infra) {
	// GC is ON by default: orphan media files and expired deleted-message
	// attachments will accumulate forever otherwise. Explicit opt-out:
	//   CHAT_MEDIA_GC_ENABLED=0
	if strings.TrimSpace(os.Getenv("CHAT_MEDIA_GC_ENABLED")) == "0" {
		log.Warn("chat media GC disabled via CHAT_MEDIA_GC_ENABLED=0")
		return
	}
	days := 30
	if v := strings.TrimSpace(os.Getenv("CHAT_MEDIA_GC_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	storageRoot := strings.TrimSpace(os.Getenv("CHAT_STORAGE_DIR"))
	if storageRoot == "" {
		storageRoot = "storage"
	}
	repo := chat.NewRepo(deps.PG)

	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 1) delete attachments whose messages were deleted long ago
				exp, err := repo.ListExpiredDeletedMessageAttachments(ctx, days, 500)
				if err != nil {
					log.Warn("chat media gc: list expired attachments", zap.Error(err))
					continue
				}
				for _, e := range exp {
					_ = repo.DeleteAttachment(ctx, e.AttachmentID)
					// legacy paths cleanup (best effort)
					if e.Path != "" {
						_ = os.Remove(e.Path)
					}
					if e.ThumbPath != nil && *e.ThumbPath != "" {
						_ = os.Remove(*e.ThumbPath)
					}
				}

				// 2) delete orphan media files (not referenced by any attachment)
				orphan, err := repo.ListOrphanMediaFiles(ctx, days, 500)
				if err != nil {
					log.Warn("chat media gc: list orphans", zap.Error(err))
					continue
				}
				for _, f := range orphan {
					// only delete inside storageRoot for safety
					p := f.Path
					if strings.HasPrefix(p, storageRoot) {
						_ = os.Remove(p)
					}
					_ = repo.DeleteMediaFile(ctx, f.ID)
				}
				if len(exp) > 0 || len(orphan) > 0 {
					log.Info("chat media gc done", zap.Int("expired_attachments", len(exp)), zap.Int("orphan_files", len(orphan)))
				}
			}
		}
	}()
}

func runMigrationsUp(log *zap.Logger, dbURL string) error {
	if strings.TrimSpace(dbURL) == "" {
		return fmt.Errorf("DATABASE_URL is empty")
	}

	log.Info("database migrations: starting (embedded SQL)")

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("sql open for migrate: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(context.Background()); err != nil {
		return fmt.Errorf("database ping before migrate: %w", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migrate iofs source: %w", err)
	}

	dbDriver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("migrate pgx driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", dbDriver)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	preVersion, preDirty, _ := m.Version()
	log.Info("database migrations: current version",
		zap.Uint("version", preVersion),
		zap.Bool("dirty", preDirty),
	)

	upErr := m.Up()
	if upErr != nil && upErr != migrate.ErrNoChange {
		if strings.Contains(upErr.Error(), "Dirty database") {
			prevVersion := parseDirtyVersion(upErr.Error())
			log.Warn("database migrations: dirty state detected, forcing previous version and retrying",
				zap.Int("force_to", prevVersion),
				zap.Error(upErr),
			)
			if forceErr := m.Force(prevVersion); forceErr != nil {
				return fmt.Errorf("force version after dirty failed: %w", forceErr)
			}
			retryErr := m.Up()
			if retryErr != nil && retryErr != migrate.ErrNoChange {
				return retryErr
			}
			postVersion, _, _ := m.Version()
			log.Info("database migrations: applied after dirty recovery", zap.Uint("version", postVersion))
			return nil
		}
		return upErr
	}

	postVersion, _, _ := m.Version()
	if upErr == migrate.ErrNoChange {
		log.Info("database migrations: no change", zap.Uint("version", postVersion))
	} else {
		log.Info("database migrations: applied", zap.Uint("from", preVersion), zap.Uint("to", postVersion))
	}
	return nil
}

// parseDirtyVersion извлекает номер версии из сообщения "Dirty database version N" и возвращает N-1 для force.
func parseDirtyVersion(errMsg string) int {
	re := regexp.MustCompile(`(?i)dirty database version\s+(\d+)`)
	if m := re.FindStringSubmatch(errMsg); len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
			return v - 1
		}
	}
	return 28 // fallback: перезапустить миграции с 29
}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/admins"
	"sarbonNew/internal/approles"
	"sarbonNew/internal/appusers"
	"sarbonNew/internal/calls"
	"sarbonNew/internal/cargo"
	"sarbonNew/internal/cargodrivers"
	"sarbonNew/internal/chat"
	"sarbonNew/internal/companies"
	"sarbonNew/internal/companytz"
	"sarbonNew/internal/config"
	"sarbonNew/internal/dispatchercompanies"
	"sarbonNew/internal/dispatcherinvitations"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/displaynames"
	"sarbonNew/internal/driverinvitations"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/drivertodispatcherinvitations"
	"sarbonNew/internal/favorites"
	"sarbonNew/internal/goadmin"
	"sarbonNew/internal/infra"
	"sarbonNew/internal/security"
	"sarbonNew/internal/server/handlers"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/server/swaggerui"
	"sarbonNew/internal/store"
	"sarbonNew/internal/telegram"
	"sarbonNew/internal/trips"
)

func NewRouter(cfg config.Config, deps *infra.Infra, logger *zap.Logger) http.Handler {
	if cfg.AppEnv == "local" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(mw.RequestLogger(logger, cfg.AppEnv == "local"))
	terminalH := handlers.NewTerminalStreamHandler(logger)

	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"*"},
	}))

	// Public endpoints that should still validate base headers.
	r.GET("/health", func(c *gin.Context) {
		resp.OKLang(c, "ok", gin.H{"status": "ok"})
	})

	// Swagger UI (OpenAPI served from local file)
	swaggerui.Register(r)
	r.GET("/terminal", terminalH.Page)
	r.POST("/terminal/login", terminalH.Login)
	r.GET("/terminal/logout", terminalH.Logout)
	r.GET("/terminal/ws", terminalH.StreamWS)

	// Вставка ссылки на кастомный CSS в страницы админки (тема не выводит CustomHeadHtml)
	r.Use(goadmin.InjectCSSMiddleware())
	// Обрезка пробелов в query-параметрах для /admin — иначе UUID с пробелом даёт pq: invalid input syntax for type uuid
	r.Use(goadmin.TrimAdminQueryMiddleware())
	// Sanitizes form posts for /admin to avoid uuid="" errors
	r.Use(goadmin.SanitizeAdminFormMiddleware())

	// GoAdmin panel at /admin (login: admin / admin)
	if cfg.DatabaseURL != "" {
		if err := goadmin.Mount(r, cfg.DatabaseURL); err != nil {
			logger.Error("goadmin mount failed", zap.Error(err))
		}
	}

	// API v1
	v1 := r.Group("/v1")
	v1.Use(mw.RequireBaseHeaders(cfg))

	driversRepo := drivers.NewRepo(deps.PG)
	dispatchersRepo := dispatchers.NewRepo(deps.PG)
	displayNameChecker := displaynames.NewChecker(deps.PG)
	adminsRepo := admins.NewRepo(deps.PG)
	companiesRepo := companies.NewRepo(deps.PG)
	appusersRepo := appusers.NewRepo(deps.PG)
	cargoRepo := cargo.NewRepo(deps.PG)
	tripsRepo := trips.NewRepo(deps.PG)
	cargoDriversRepo := cargodrivers.NewRepo(deps.PG)
	dcrRepo := dispatchercompanies.NewRepo(deps.PG)
	dispInvRepo := dispatcherinvitations.NewRepo(deps.PG)
	driverInvRepo := driverinvitations.NewRepo(deps.PG)
	jwtm := security.NewJWTManager(cfg.JWTSigningKey, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)

	otpVerifyWindow := time.Duration(cfg.OTPVerifyWindowSeconds) * time.Second
	otpStore := store.NewOTPStore(
		deps.Redis,
		cfg.JWTSigningKey,
		cfg.OTPTTL,
		cfg.OTPResendCooldown,
		cfg.OTPMaxAttempts,
		int64(cfg.OTPSendLimitPerPhonePerHour),
		int64(cfg.OTPSendLimitPerIPPerHour),
		cfg.OTPSendWindow,
		int64(cfg.OTPVerifyAttemptsPerPhone),
		otpVerifyWindow,
	)
	companyUserOTPStore := store.NewOTPStoreWithPrefix(
		deps.Redis,
		cfg.JWTSigningKey,
		cfg.OTPTTL,
		cfg.OTPResendCooldown,
		cfg.OTPMaxAttempts,
		int64(cfg.OTPSendLimitPerPhonePerHour),
		int64(cfg.OTPSendLimitPerIPPerHour),
		cfg.OTPSendWindow,
		int64(cfg.OTPVerifyAttemptsPerPhone),
		otpVerifyWindow,
		"company_",
	)
	sessionStore := store.NewSessionStore(deps.Redis, 15*time.Minute)
	refreshStore := store.NewRefreshStore(deps.Redis, cfg.JWTRefreshTTL, cfg.JWTAccessTTL)
	tgClient := telegram.NewGatewayClient(cfg.TelegramGatewayBaseURL, cfg.TelegramGatewayToken, cfg.TelegramGatewaySenderID, cfg.TelegramGatewayBypass)
	phoneChangeStore := store.NewPhoneChangeStore(deps.Redis, cfg.JWTSigningKey, cfg.OTPTTL, cfg.OTPMaxAttempts)

	dispRegSessions := store.NewDispatcherSessionStore(deps.Redis, "disp_regsession", 15*time.Minute)
	companyUserRegSessions := store.NewDispatcherSessionStore(deps.Redis, "company_regsession", 15*time.Minute)
	dispResetActions := store.NewDispatcherOTPActionStore(deps.Redis, cfg.JWTSigningKey, "disp_reset", cfg.OTPTTL, cfg.OTPMaxAttempts)
	dispPhoneActions := store.NewDispatcherOTPActionStore(deps.Redis, cfg.JWTSigningKey, "disp_phone", cfg.OTPTTL, cfg.OTPMaxAttempts)

	authH := handlers.NewAuthHandler(logger, driversRepo, otpStore, sessionStore, refreshStore, jwtm, tgClient, cfg.OTPTTL, cfg.OTPLength)
	regH := handlers.NewRegistrationHandler(logger, driversRepo, displayNameChecker, sessionStore, jwtm, refreshStore)
	kycH := handlers.NewKYCHandler(logger, driversRepo)
	profileH := handlers.NewProfileHandler(logger, driversRepo, displayNameChecker, phoneChangeStore, tgClient, cfg.OTPTTL, cfg.OTPLength)

	dispAuthH := handlers.NewDispatcherAuthHandler(logger, dispatchersRepo, otpStore, dispRegSessions, dispResetActions, jwtm, refreshStore, tgClient, cfg.OTPTTL, cfg.OTPLength)
	dispRegH := handlers.NewDispatcherRegistrationHandler(logger, dispatchersRepo, displayNameChecker, dispRegSessions, jwtm, refreshStore)
	dispProfileH := handlers.NewDispatcherProfileHandler(logger, dispatchersRepo, displayNameChecker, dispPhoneActions, tgClient, cfg.OTPTTL, cfg.OTPLength)
	adminAuthH := handlers.NewAdminAuthHandler(logger, adminsRepo, jwtm, refreshStore)
	adminCompaniesH := handlers.NewAdminCompaniesHandler(logger, companiesRepo, appusersRepo, cfg)
	cargoH := handlers.NewCargoHandler(logger, cargoRepo, tripsRepo, driversRepo, jwtm, cfg)
	dispCargoExportH := handlers.NewDispatcherCargoExportHandler(logger, cargoRepo)
	adminCargoModH := handlers.NewAdminCargoModerationHandler(logger, cargoRepo)
	dispCompaniesH := handlers.NewDispatcherCompaniesHandler(logger, companiesRepo, dcrRepo, jwtm)
	dispInvH := handlers.NewDispatcherInvitationsHandler(logger, dispInvRepo, dcrRepo, dispatchersRepo)
	driverInvH := handlers.NewDriverInvitationsHandler(logger, driverInvRepo, dcrRepo, driversRepo)
	driverDispH := handlers.NewDriverDispatchersHandler(logger, driversRepo, dispatchersRepo, dcrRepo)
	driverDispCatalogH := handlers.NewDriverDispatchersCatalogHandler(logger, dispatchersRepo)
	d2dInvRepo := drivertodispatcherinvitations.NewRepo(deps.PG)
	d2dInvH := handlers.NewDriverToDispatcherInvitationsHandler(logger, d2dInvRepo, driversRepo, dispatchersRepo)
	tripsH := handlers.NewTripsHandler(logger, tripsRepo, cargoRepo)

	cargoDriversH := handlers.NewCargoDriversHandler(logger, cargoRepo, cargoDriversRepo)

	favRepo := favorites.NewRepo(deps.PG)
	favH := handlers.NewFavoritesHandler(logger, favRepo, cargoRepo, driversRepo, dispatchersRepo)

	driverCargoSearchH := handlers.NewDriverCargoSearchHandler(logger, cargoRepo, driversRepo)

	chatRepo := chat.NewRepo(deps.PG)
	chatPresence := chat.NewPresenceStore(deps.Redis)
	chatHub := chat.NewHub(chatPresence, logger)
	chatH := handlers.NewChatHandler(logger, chatRepo, chatPresence, chatHub, driversRepo, dispatchersRepo)

	callsRepo := calls.NewRepo(deps.PG)
	callsLimiter := calls.NewCreateLimiter(deps.Redis, cfg.CallsCreateLimit, cfg.CallsCreateWindow)
	callsH := handlers.NewCallsHandler(logger, callsRepo, chatRepo, chatHub, callsLimiter, cfg.CallsRingingTimeout)
	chatHub.SetOnUserConnected(func(userID uuid.UUID) {
		// WS reconnect recovery: clear stale call state for this user.
		list, err := callsRepo.ListOngoingForUser(context.Background(), userID)
		if err != nil {
			return
		}
		now := time.Now()
		timeout := cfg.CallsRingingTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		for _, c := range list {
			peerID := c.CallerID
			if peerID == userID {
				peerID = c.CalleeID
			}
			switch c.Status {
			case calls.StatusRinging:
				if now.Sub(c.CreatedAt) > timeout {
					_, _ = callsRepo.MissIfRingingSystem(context.Background(), c.ID, "recovered_timeout")
				}
			case calls.StatusActive:
				if !chatHub.IsOnline(peerID) {
					_, _ = callsRepo.EndIfActiveSystem(context.Background(), c.ID, "peer_offline_recovered")
				}
			}
		}
	})

	// WebRTC/call signaling routing: requires call_id in payload and validates participants.
	chatHub.SetOnCallSignal(func(fromUserID uuid.UUID, msgType string, data json.RawMessage) (uuid.UUID, []byte, bool) {
		var body struct {
			CallID  string          `json:"call_id"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &body) != nil || strings.TrimSpace(body.CallID) == "" {
			return uuid.Nil, nil, false
		}
		callID, err := uuid.Parse(strings.TrimSpace(body.CallID))
		if err != nil || callID == uuid.Nil {
			return uuid.Nil, nil, false
		}
		call, err := callsRepo.GetForUser(context.Background(), callID, fromUserID)
		if err != nil || call == nil {
			return uuid.Nil, nil, false
		}
		// Only allow signaling for ringing/active calls.
		if call.Status != calls.StatusRinging && call.Status != calls.StatusActive {
			return uuid.Nil, nil, false
		}
		peerID := call.CallerID
		if peerID == fromUserID {
			peerID = call.CalleeID
		}
		out, _ := json.Marshal(map[string]any{
			"type": msgType,
			"data": map[string]any{
				"call_id": callID.String(),
				"from_id": fromUserID.String(),
				"payload": body.Payload,
			},
		})
		return peerID, out, true
	})

	approlesRepo := approles.NewRepo(deps.PG)
	ucrRepo := companytz.NewRepoUCR(deps.PG)
	invitationsRepo := companytz.NewRepoInvitations(deps.PG)
	auditRepo := companytz.NewRepoAudit(deps.PG)
	companyUserAuthH := handlers.NewCompanyUserAuthHandler(logger, appusersRepo, companyUserOTPStore, companyUserRegSessions, jwtm, refreshStore, tgClient, cfg.OTPTTL, cfg.OTPLength)
	companyUserRegH := handlers.NewCompanyUserRegistrationHandler(logger, appusersRepo, companyUserRegSessions, jwtm, refreshStore)
	companyTZH := handlers.NewCompanyTZHandler(logger, appusersRepo, companiesRepo, approlesRepo, ucrRepo, invitationsRepo, auditRepo, jwtm)

	v1.POST("/company-users/auth/phone", companyUserAuthH.SendOTP)
	v1.POST("/company-users/auth/otp/verify", companyUserAuthH.VerifyOTP)
	v1.POST("/company-users/auth/refresh", authH.Refresh) // company user: обновить пару токенов по refresh_token
	v1.POST("/company-users/registration/complete", companyUserRegH.Complete)

	// Driver: только API водителя (auth, registration, profile, trips, invitations)
	v1.POST("/driver/auth/phone", authH.SendOTP)
	v1.POST("/driver/auth/otp/verify", authH.VerifyOTP)
	v1.POST("/driver/auth/refresh", authH.Refresh)
	v1.POST("/driver/auth/logout", authH.Logout)
	v1.POST("/driver/registration/start", regH.Start)
	v1.GET("/driver/transport-options", handlers.GetTransportOptions)

	// Reference: справочники (общие для водителя, диспетчера и др.)
	v1.GET("/reference/drivers", handlers.GetReferenceDrivers)
	v1.GET("/reference/cargo", handlers.GetReferenceCargo)
	v1.GET("/reference/company", handlers.GetReferenceCompany(approlesRepo))
	v1.GET("/reference/admin", handlers.GetReferenceAdmin)
	v1.GET("/reference/dispatchers", handlers.GetReferenceDispatchers)
	v1.GET("/reference/cities", handlers.GetReferenceCities())
	v1.GET("/reference/countries", handlers.GetReferenceCountries())
	v1.GET("/reference/cargo-types/hint", handlers.HintCargoTypes(deps.PG))

	// API /api/cargo (same base headers as v1)
	api := r.Group("/api")
	api.Use(mw.RequireBaseHeaders(cfg))
	// До /cargo/:id — иначе Gin воспринимает "photos" как :id
	api.POST("/cargo/photos", cargoH.UploadPendingCargoPhoto)
	api.GET("/cargo/photos/:photoId", cargoH.GetPendingCargoPhoto)
	api.POST("/cargo", cargoH.Create)
	api.GET("/cargo", cargoH.List)
	api.GET("/cargo/:id", cargoH.GetByID)
	api.PUT("/cargo/:id", cargoH.Update)
	api.DELETE("/cargo/:id", cargoH.Delete)
	api.PATCH("/cargo/:id/status", cargoH.PatchStatus)
	api.POST("/cargo/:id/offers", cargoH.CreateOffer)
	api.GET("/cargo/:id/offers", cargoH.ListOffers)
	api.POST("/cargo/:id/photos", cargoH.UploadPhoto)
	api.GET("/cargo/:id/photos", cargoH.ListPhotos)
	api.GET("/cargo/:id/photos/:photoId", cargoH.GetPhoto)
	api.DELETE("/cargo/:id/photos/:photoId", cargoH.DeletePhoto)
	api.POST("/offers/:id/accept", cargoH.AcceptOffer)
	api.GET("/trips", tripsH.List)
	api.GET("/trips/:id", tripsH.Get)

	v1.POST("/dispatchers/auth/phone", dispAuthH.SendOTP)
	v1.POST("/dispatchers/auth/otp/verify", dispAuthH.VerifyOTP)
	v1.POST("/dispatchers/auth/login/password", dispAuthH.LoginPassword)
	v1.POST("/dispatchers/auth/refresh", authH.Refresh) // диспетчер: обновить пару токенов по refresh_token
	v1.POST("/dispatchers/auth/reset-password/request", dispAuthH.ResetPasswordRequest)
	v1.POST("/dispatchers/auth/reset-password/confirm", dispAuthH.ResetPasswordConfirm)
	v1.POST("/dispatchers/auth/logout", dispAuthH.Logout)
	v1.POST("/dispatchers/registration/complete", dispRegH.Complete)

	// Admin auth (login by password, refresh) — только base headers; без admin token
	v1.POST("/admin/auth/login/password", adminAuthH.LoginPassword)
	v1.POST("/admin/auth/refresh", authH.Refresh) // админ: обновить пару токенов по refresh_token

	// Все маршруты под adminAuthed проверяют: base headers (X-Client-Token, X-Device-Type, X-Language) + X-User-Token с role=admin
	adminAuthed := v1.Group("/admin")
	adminAuthed.Use(mw.RequireAdmin(jwtm, refreshStore))
	adminAuthed.POST("/companies", adminCompaniesH.Create)
	adminAuthed.PATCH("/companies/:id/owner", adminCompaniesH.SetOwner)
	adminAuthed.GET("/company-users/owners/search", adminCompaniesH.SearchOwners)
	adminAuthed.GET("/cargo/moderation", adminCargoModH.ListPending)
	adminAuthed.POST("/cargo/:id/moderation/accept", adminCargoModH.Accept)
	adminAuthed.POST("/cargo/:id/moderation/reject", adminCargoModH.Reject)

	driverAuthed := v1.Group("/driver")
	driverAuthed.Use(mw.RequireDriver(jwtm, refreshStore))
	driverAuthed.Use(mw.UpdateDriverLastOnline(driversRepo))
	driverAuthed.GET("/profile", profileH.Get)
	driverAuthed.PATCH("/profile/driver", profileH.PatchDriver)
	driverAuthed.PUT("/profile/heartbeat", profileH.Heartbeat)
	driverAuthed.POST("/profile/photo", profileH.UploadPhoto)
	driverAuthed.GET("/profile/photo", profileH.GetPhoto)
	driverAuthed.DELETE("/profile/photo", profileH.DeletePhoto)
	driverAuthed.POST("/profile/phone-change/request", profileH.PhoneChangeRequest)
	driverAuthed.POST("/profile/phone-change/verify", profileH.PhoneChangeVerify)
	driverAuthed.PATCH("/profile/power", profileH.PatchPower)
	driverAuthed.PATCH("/profile/trailer", profileH.PatchTrailer)
	driverAuthed.DELETE("/profile", profileH.Delete)
	driverAuthed.PATCH("/registration/geo-push", regH.GeoPush)
	driverAuthed.PATCH("/registration/transport-type", regH.TransportType)
	driverAuthed.PATCH("/kyc", kycH.Submit)
	driverAuthed.GET("/trips", tripsH.ListMy)
	driverAuthed.POST("/trips/:id/confirm", tripsH.DriverConfirm)
	driverAuthed.POST("/trips/:id/confirm-transition", tripsH.ConfirmTransitionDriver)
	driverAuthed.POST("/trips/:id/reject", tripsH.DriverReject)
	driverAuthed.POST("/trips/:id/cancel", tripsH.CancelTripDriver)
	driverAuthed.GET("/trips/:id/state", tripsH.TripStateDriver)
	driverAuthed.GET("/driver-invitations", driverInvH.ListInvitations)
	driverAuthed.POST("/driver-invitations/accept", driverInvH.Accept)
	driverAuthed.POST("/driver-invitations/decline", driverInvH.Decline)
	driverAuthed.GET("/dispatchers", driverDispH.ListMyDispatchers)
	driverAuthed.GET("/dispatchers/catalog", driverDispCatalogH.ListCatalog)
	driverAuthed.GET("/dispatchers/hint", driverDispCatalogH.HintByPhone)
	driverAuthed.GET("/user-finder", chatH.UserFinder)
	driverAuthed.DELETE("/dispatchers/:dispatcherId", driverDispH.UnlinkDispatcher)
	driverAuthed.GET("/dispatcher-invitations", d2dInvH.ListSentByDriver)
	driverAuthed.POST("/dispatcher-invitations", d2dInvH.CreateFromDriver)
	driverAuthed.DELETE("/dispatcher-invitations/:token", d2dInvH.CancelByDriver)
	driverAuthed.POST("/cargo-likes", favH.AddDriverFavoriteCargo)
	driverAuthed.DELETE("/cargo-likes/:cargoId", favH.DeleteDriverFavoriteCargo)
	driverAuthed.GET("/cargo-likes", favH.ListDriverFavoriteCargo)
	driverAuthed.POST("/dispatcher-likes", favH.AddDriverFavoriteDispatcher)
	driverAuthed.DELETE("/dispatcher-likes/:dispatcherId", favH.DeleteDriverFavoriteDispatcher)
	driverAuthed.GET("/dispatcher-likes", favH.ListDriverFavoriteDispatchers)
	driverAuthed.POST("/favorite-cargo", favH.AddDriverFavoriteCargo)
	driverAuthed.DELETE("/favorite-cargo/:cargoId", favH.DeleteDriverFavoriteCargo)
	driverAuthed.GET("/favorite-cargo", favH.ListDriverFavoriteCargo)
	driverAuthed.GET("/matching-cargo", driverCargoSearchH.MatchingCargoForDriver)
	driverAuthed.POST("/cargo/active", cargoH.ListActiveCargoForDriver)
	driverAuthed.GET("/active-cargo", cargoDriversH.GetMyActiveCargo)
	driverAuthed.GET("/cargo-offers", cargoH.ListMyCargoOffers)
	driverAuthed.GET("/offers/all", cargoH.ListOffersForDriver)
	driverAuthed.POST("/cargo/:id/offers", cargoH.DriverCreateOffer)
	driverAuthed.POST("/offers/:id/accept", cargoH.AcceptOffer)
	driverAuthed.POST("/offers/:id/reject", cargoH.RejectOfferDriver)
	driverAuthed.POST("/offers/:id/cancel", cargoH.CancelOfferOrTripDriver)

	// Dispatchers: только API диспетчера
	dispAuthed := v1.Group("/dispatchers")
	dispAuthed.Use(mw.RequireDispatcher(jwtm, refreshStore))
	dispAuthed.Use(mw.UpdateDispatcherLastOnline(dispatchersRepo))
	dispAuthed.GET("/profile", dispProfileH.Get)
	dispAuthed.PATCH("/profile", dispProfileH.Patch)
	dispAuthed.POST("/profile/photo", dispProfileH.UploadPhoto)
	dispAuthed.GET("/profile/photo", dispProfileH.GetPhoto)
	dispAuthed.DELETE("/profile/photo", dispProfileH.DeletePhoto)
	dispAuthed.PUT("/profile/password", dispProfileH.ChangePassword)
	dispAuthed.POST("/profile/phone-change/request", dispProfileH.PhoneChangeRequest)
	dispAuthed.POST("/profile/phone-change/verify", dispProfileH.PhoneChangeVerify)
	dispAuthed.DELETE("/profile", dispProfileH.Delete)
	// Freelance: no create company; list/switch only when invited. Cargo/offers/trips via /api and below.
	dispAuthed.GET("/companies", dispCompaniesH.ListMyCompanies)
	dispAuthed.POST("/auth/switch-company", dispCompaniesH.SwitchCompany)
	dispAuthed.POST("/companies/:companyId/invitations", dispInvH.CreateInvitation)
	dispAuthed.POST("/invitations/accept", dispInvH.Accept)
	dispAuthed.POST("/invitations/decline", dispInvH.Decline)
	dispAuthed.GET("/driver-invitations", driverInvH.ListSent)
	dispAuthed.POST("/driver-invitations", driverInvH.CreateForFreelance)
	dispAuthed.DELETE("/driver-invitations/:token", driverInvH.CancelInvitation)
	dispAuthed.POST("/companies/:companyId/driver-invitations", driverInvH.Create)
	dispAuthed.GET("/drivers/find", driverInvH.FindDrivers)
	dispAuthed.GET("/drivers", driverInvH.ListMyDrivers)
	dispAuthed.GET("/drivers/all", driverInvH.ListAllDriversForFreelance)
	dispAuthed.DELETE("/drivers/:driverId", driverInvH.UnlinkDriver)
	dispAuthed.GET("/invitations-from-drivers", d2dInvH.ListReceivedByDispatcher)
	dispAuthed.POST("/invitations-from-drivers/accept", d2dInvH.AcceptByDispatcher)
	dispAuthed.POST("/invitations-from-drivers/decline", d2dInvH.DeclineByDispatcher)
	dispAuthed.PUT("/drivers/:driverId/power", driverInvH.SetDriverPower)
	dispAuthed.PUT("/drivers/:driverId/trailer", driverInvH.SetDriverTrailer)
	dispAuthed.PATCH("/trips/:id/assign-driver", tripsH.AssignDriver)
	dispAuthed.POST("/trips/:id/confirm-transition", tripsH.ConfirmTransitionDispatcher)
	dispAuthed.POST("/trips/:id/cancel", tripsH.CancelTripDispatcher)
	dispAuthed.GET("/trips/:id/state", tripsH.TripStateDispatcher)
	dispAuthed.POST("/offers/:id/accept", cargoH.AcceptOffer)
	dispAuthed.POST("/offers/:id/reject", cargoH.RejectOfferDispatcher)
	dispAuthed.GET("/offers/all", cargoH.ListSentOffersForDispatcher)
	dispAuthed.GET("/cargo/mine", cargoH.ListMyCargoForDispatcher)
	dispAuthed.GET("/cargo/all", cargoH.ListAllCargoForDispatcher)
	dispAuthed.GET("/cargo/:id/negotiation", cargoH.ListCargoNegotiation)
	dispAuthed.GET("/cargo/:id/drivers", cargoDriversH.ListByCargo)
	dispAuthed.POST("/cargo/:id/drivers/remove", cargoDriversH.RemoveFromCargo)
	dispAuthed.GET("/cargo/export.xlsx", dispCargoExportH.ExportMyCargoExcel)
	dispAuthed.POST("/cargo-likes", favH.AddDispatcherFavoriteCargo)
	dispAuthed.DELETE("/cargo-likes/:cargoId", favH.DeleteDispatcherFavoriteCargo)
	dispAuthed.GET("/cargo-likes", favH.ListDispatcherFavoriteCargo)
	dispAuthed.POST("/driver-likes", favH.AddDispatcherFavoriteDriver)
	dispAuthed.DELETE("/driver-likes/:driverId", favH.DeleteDispatcherFavoriteDriver)
	dispAuthed.GET("/driver-likes", favH.ListDispatcherFavoriteDrivers)
	dispAuthed.POST("/favorite-cargo", favH.AddDispatcherFavoriteCargo)
	dispAuthed.DELETE("/favorite-cargo/:cargoId", favH.DeleteDispatcherFavoriteCargo)
	dispAuthed.GET("/favorite-cargo", favH.ListDispatcherFavoriteCargo)
	dispAuthed.POST("/favorite-drivers", favH.AddDispatcherFavoriteDriver)
	dispAuthed.DELETE("/favorite-drivers/:driverId", favH.DeleteDispatcherFavoriteDriver)
	dispAuthed.GET("/favorite-drivers", favH.ListDispatcherFavoriteDrivers)
	dispAuthed.GET("/user-finder", chatH.UserFinder)

	// Company users (company_users): OTP auth, companies, invitations
	appUserAuthed := v1.Group("")
	appUserAuthed.Use(mw.RequireAppUser(jwtm, refreshStore))
	appUserAuthed.GET("/auth/companies", companyTZH.ListMyCompanies)
	appUserAuthed.POST("/auth/switch-company", companyTZH.SwitchCompany)
	appUserAuthed.POST("/companies", companyTZH.CreateCompany)
	appUserAuthed.POST("/companies/:companyId/invitations", companyTZH.CreateInvitation)
	appUserAuthed.POST("/invitations/accept", companyTZH.AcceptInvitation)
	appUserAuthed.GET("/companies/:companyId/users", companyTZH.ListCompanyUsers)
	appUserAuthed.PUT("/companies/:companyId/users/:userId/role", companyTZH.UpdateUserRole)
	appUserAuthed.DELETE("/companies/:companyId/users/:userId", companyTZH.RemoveUser)

	// Chat (driver, dispatcher, admin): JWT auth only; WS uses ?token=JWT.
	chatGroup := v1.Group("/chat")
	chatGroup.Use(mw.RequireChatUser(jwtm, refreshStore))
	chatGroup.GET("/user-finder", chatH.UserFinder)
	chatGroup.GET("/users/:id/photo", chatH.GetPeerPhoto)
	chatGroup.GET("/conversations", chatH.ListConversations)
	chatGroup.POST("/conversations", chatH.GetOrCreateConversation)
	chatGroup.POST("/conversations/:id/read", chatH.MarkConversationRead)
	chatGroup.GET("/conversations/:id/messages", chatH.ListMessages)
	chatGroup.POST("/conversations/:id/messages", chatH.SendMessage)
	chatGroup.POST("/conversations/:id/messages/media", chatH.SendMediaMessage)
	chatGroup.PATCH("/messages/:id", chatH.EditMessage)
	chatGroup.DELETE("/messages/:id", chatH.DeleteMessage)
	chatGroup.GET("/presence/:user_id", chatH.GetPresence)
	chatGroup.GET("/files/:id", chatH.GetFile)
	chatGroup.GET("/ws", chatH.ServeWS)

	// Calls (voice): state/session in REST; signaling via chat ws (webrtc.* / call.*).
	callsGroup := v1.Group("/calls")
	callsGroup.Use(mw.RequireChatUser(jwtm, refreshStore))
	callsGroup.GET("", callsH.ListMyCalls)
	callsGroup.GET("/ice-servers", func(c *gin.Context) {
		raw := strings.TrimSpace(cfg.CallsICEURLs)
		if raw == "" {
			raw = "stun:stun.l.google.com:19302"
		}
		parts := strings.Split(raw, ",")
		urls := make([]string, 0, len(parts))
		for _, p := range parts {
			u := strings.TrimSpace(p)
			if u != "" {
				urls = append(urls, u)
			}
		}
		ice := gin.H{"urls": urls}
		if cfg.CallsICEUsername != "" {
			ice["username"] = cfg.CallsICEUsername
		}
		if cfg.CallsICECredential != "" {
			ice["credential"] = cfg.CallsICECredential
		}
		resp.OKLang(c, "ok", gin.H{
			"ice_servers": []gin.H{ice},
		})
	})
	callsGroup.GET("/test/bootstrap", callsH.GetCallTestBootstrap)
	callsGroup.POST("", callsH.CreateCall)
	callsGroup.GET("/:id", callsH.GetCall)
	callsGroup.POST("/:id/accept", callsH.AcceptCall)
	callsGroup.POST("/:id/decline", callsH.DeclineCall)
	callsGroup.POST("/:id/cancel", callsH.CancelCall)
	callsGroup.POST("/:id/end", callsH.EndCall)

	return r
}

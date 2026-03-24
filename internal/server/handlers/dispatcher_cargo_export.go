package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"

	"sarbonNew/internal/cargo"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
)

// DispatcherCargoExportHandler: экспорт «моих» грузов диспетчера в Excel.
type DispatcherCargoExportHandler struct {
	logger *zap.Logger
	repo   *cargo.Repo
}

func NewDispatcherCargoExportHandler(logger *zap.Logger, repo *cargo.Repo) *DispatcherCargoExportHandler {
	return &DispatcherCargoExportHandler{logger: logger, repo: repo}
}

func cargoListFilterFromQueryForDispatcherExport(c *gin.Context) cargo.ListFilter {
	f := cargo.ListFilter{
		Sort:        c.DefaultQuery("sort", "created_at:desc"),
		TruckType:   strings.TrimSpace(c.Query("truck_type")),
		CreatedFrom: strings.TrimSpace(c.Query("created_from")),
		CreatedTo:   strings.TrimSpace(c.Query("created_to")),
	}
	if v := c.Query("status"); v != "" {
		f.Status = strings.Split(v, ",")
		for i := range f.Status {
			f.Status[i] = strings.TrimSpace(strings.ToUpper(f.Status[i]))
		}
	}
	if v := c.Query("weight_min"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.WeightMin = &n
		}
	}
	if v := c.Query("weight_max"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.WeightMax = &n
		}
	}
	if v := c.Query("with_offers"); v != "" {
		b := strings.ToLower(v) == "true" || v == "1"
		f.WithOffers = &b
	}
	return f
}

func normalizeExportLang(h string) string {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "ru", "uz", "en", "tr", "zh":
		return strings.ToLower(strings.TrimSpace(h))
	default:
		return "en"
	}
}

func exportColumnTitles(lang string) []string {
	switch lang {
	case "ru":
		return []string{
			"№", "ID", "Название", "Статус", "Вес (т)", "Объём (м³)", "Грузоподъёмность (т)",
			"Тип ТС", "Тип груза", "Контакт", "Телефон", "Компания (ID)",
			"Создан", "Погрузка", "Выгрузка", "Оплата", "Готовность", "ADR",
			"Упаковка", "Габариты",
		}
	case "uz":
		return []string{
			"№", "ID", "Nomi", "Holat", "Og'irlik (t)", "Hajm (m³)", "Yuk ko'tarish (t)",
			"Transport turi", "Yuk turi", "Kontakt", "Telefon", "Kompaniya (ID)",
			"Yaratilgan", "Yuklash", "Tushirish", "To'lov", "Tayyorlik", "ADR",
			"O'ram", "O'lchamlar",
		}
	case "tr":
		return []string{
			"No", "ID", "Ad", "Durum", "Ağırlık (t)", "Hacim (m³)", "Kapasite (t)",
			"Araç tipi", "Yük tipi", "İletişim", "Telefon", "Şirket (ID)",
			"Oluşturuldu", "Yükleme", "Boşaltma", "Ödeme", "Hazırlık", "ADR",
			"Ambalaj", "Boyutlar",
		}
	case "zh":
		return []string{
			"序号", "ID", "名称", "状态", "重量(吨)", "体积(m³)", "载重要求(吨)",
			"车型", "货物类型", "联系人", "电话", "公司(ID)",
			"创建时间", "装货点", "卸货点", "运费", "就绪时间", "ADR",
			"包装", "尺寸",
		}
	default: // en
		return []string{
			"#", "ID", "Name", "Status", "Weight (t)", "Volume (m³)", "Capacity (t)",
			"Truck type", "Cargo type", "Contact", "Phone", "Company ID",
			"Created at", "Load points", "Unload points", "Payment", "Ready at", "ADR",
			"Packaging", "Dimensions",
		}
	}
}

func cargoTypeNameForLang(c *cargo.Cargo, lang string) string {
	switch lang {
	case "ru":
		if c.CargoTypeNameRU != nil {
			return *c.CargoTypeNameRU
		}
	case "uz":
		if c.CargoTypeNameUZ != nil {
			return *c.CargoTypeNameUZ
		}
	case "tr":
		if c.CargoTypeNameTR != nil {
			return *c.CargoTypeNameTR
		}
	case "zh":
		if c.CargoTypeNameZH != nil {
			return *c.CargoTypeNameZH
		}
	default:
		if c.CargoTypeNameEN != nil {
			return *c.CargoTypeNameEN
		}
	}
	if c.CargoTypeCode != nil {
		return *c.CargoTypeCode
	}
	return ""
}

func routeAddressesByType(rps []cargo.RoutePoint, typ string) string {
	var parts []string
	for _, rp := range rps {
		if strings.EqualFold(strings.TrimSpace(rp.Type), typ) {
			a := strings.TrimSpace(rp.Address)
			if a != "" {
				parts = append(parts, a)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func formatPaymentCell(p *cargo.Payment) string {
	if p == nil {
		return ""
	}
	if p.PriceRequest {
		return "price_request"
	}
	if p.IsNegotiable {
		return "negotiable"
	}
	if p.TotalAmount != nil {
		cur := ""
		if p.TotalCurrency != nil {
			cur = *p.TotalCurrency
		}
		return fmt.Sprintf("%.2f %s", *p.TotalAmount, strings.TrimSpace(cur))
	}
	return ""
}

func exportStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func ptrTimeRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ExportMyCargoExcel GET /v1/dispatchers/cargo/export.xlsx
// Те же query-фильтры, что GET /api/cargo (status, weight_min/max, truck_type, created_from/to, with_offers, sort).
// Без page/limit — выгружаются все подходящие грузы диспетчера (макс. cargo.MaxCargoExportRows).
func (h *DispatcherCargoExportHandler) ExportMyCargoExcel(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	lang := normalizeExportLang(c.GetHeader(mw.HeaderLanguage))
	filter := cargoListFilterFromQueryForDispatcherExport(c)

	items, total, err := h.repo.ListDispatcherCargoForExport(c.Request.Context(), dispatcherID, filter)
	if err != nil {
		if err == cargo.ErrCargoExportTooManyRows {
			resp.ErrorWithDataLang(c, http.StatusBadRequest, "cargo_export_too_many_rows", gin.H{
				"max":   cargo.MaxCargoExportRows,
				"total": total,
			})
			return
		}
		h.logger.Error("dispatcher cargo export list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "cargo_export_failed")
		return
	}

	ids := make([]uuid.UUID, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	routesByCargo, err := h.repo.GetRoutePointsForCargoIDs(c.Request.Context(), ids)
	if err != nil {
		h.logger.Error("dispatcher cargo export routes", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "cargo_export_failed")
		return
	}
	payByCargo, err := h.repo.GetPaymentsForCargoIDs(c.Request.Context(), ids)
	if err != nil {
		h.logger.Error("dispatcher cargo export payments", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "cargo_export_failed")
		return
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet = "Cargo"
	_ = f.SetSheetName("Sheet1", sheet)

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#1F4E78", Style: 1},
			{Type: "top", Color: "#1F4E78", Style: 1},
			{Type: "right", Color: "#1F4E78", Style: 1},
			{Type: "bottom", Color: "#1F4E78", Style: 1},
		},
	})
	if err != nil {
		h.logger.Error("excelize header style", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "cargo_export_failed")
		return
	}

	cellStyle, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#D0D0D0", Style: 1},
			{Type: "top", Color: "#D0D0D0", Style: 1},
			{Type: "right", Color: "#D0D0D0", Style: 1},
			{Type: "bottom", Color: "#D0D0D0", Style: 1},
		},
	})

	titles := exportColumnTitles(lang)
	for i, title := range titles {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, title)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	widths := []float64{5, 38, 28, 16, 10, 10, 14, 12, 22, 18, 16, 38, 20, 36, 36, 18, 20, 8, 22, 18}
	for i, w := range widths {
		if i < len(titles) {
			col, _ := excelize.ColumnNumberToName(i + 1)
			_ = f.SetColWidth(sheet, col, col, w)
		}
	}

	for rowIdx, item := range items {
		r := rowIdx + 2
		rps := routesByCargo[item.ID]
		pay := payByCargo[item.ID]

		capStr := ""
		if item.CapacityRequired != nil {
			capStr = fmt.Sprintf("%.3g", *item.CapacityRequired)
		}
		adr := ""
		if item.ADREnabled {
			adr = "yes"
			if item.ADRClass != nil && *item.ADRClass != "" {
				adr = *item.ADRClass
			}
		}
		name := ""
		if item.Name != nil {
			name = *item.Name
		}
		company := ""
		if item.CompanyID != nil {
			company = item.CompanyID.String()
		}

		vals := []any{
			rowIdx + 1,
			item.ID.String(),
			name,
			string(item.Status),
			item.Weight,
			item.Volume,
			capStr,
			item.TruckType,
			cargoTypeNameForLang(&item, lang),
			exportStrPtr(item.ContactName),
			exportStrPtr(item.ContactPhone),
			company,
			item.CreatedAt.UTC().Format(time.RFC3339),
			routeAddressesByType(rps, "LOAD"),
			routeAddressesByType(rps, "UNLOAD"),
			formatPaymentCell(pay),
			ptrTimeRFC3339(item.ReadyAt),
			adr,
			exportStrPtr(item.Packaging),
			exportStrPtr(item.Dimensions),
		}
		for colIdx, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, r)
			_ = f.SetCellValue(sheet, cell, v)
			_ = f.SetCellStyle(sheet, cell, cell, cellStyle)
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		h.logger.Error("dispatcher cargo export write buffer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "cargo_export_failed")
		return
	}

	fname := fmt.Sprintf("my_cargo_%s.xlsx", time.Now().UTC().Format("20060102_150405"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Header("X-Export-Total", strconv.Itoa(total))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

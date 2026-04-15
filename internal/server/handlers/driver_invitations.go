package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sarbonNew/internal/dispatchercompanies"
	"sarbonNew/internal/dispatchers"
	"sarbonNew/internal/domain"
	"sarbonNew/internal/driverinvitations"
	"sarbonNew/internal/drivers"
	"sarbonNew/internal/drivertodispatcherinvitations"
	"sarbonNew/internal/server/mw"
	"sarbonNew/internal/server/resp"
	"sarbonNew/internal/tripnotif"
	"sarbonNew/internal/userstream"
)

type DriverInvitationsHandler struct {
	logger *zap.Logger
	repo   *driverinvitations.Repo
	d2d    *drivertodispatcherinvitations.Repo
	dcr    *dispatchercompanies.Repo
	drv    *drivers.Repo
	disp   *dispatchers.Repo
	stream *userstream.Hub
}

func NewDriverInvitationsHandler(logger *zap.Logger, repo *driverinvitations.Repo, d2d *drivertodispatcherinvitations.Repo, dcr *dispatchercompanies.Repo, drv *drivers.Repo, disp *dispatchers.Repo, stream *userstream.Hub) *DriverInvitationsHandler {
	return &DriverInvitationsHandler{logger: logger, repo: repo, d2d: d2d, dcr: dcr, drv: drv, disp: disp, stream: stream}
}

// CreateDriverInvitationReq body for POST /v1/dispatchers/companies/:companyId/driver-invitations
type CreateDriverInvitationReq struct {
	DriverID uuid.UUID `json:"driver_id" binding:"required"`
}

// CreateDriverInvitation creates invitation for driver by phone (dispatcher with company access).
func (h *DriverInvitationsHandler) Create(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	companyID, _ := uuid.Parse(c.Param("companyId"))
	if companyID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_company_id")
		return
	}
	ok, err := h.dcr.HasAccess(c.Request.Context(), dispatcherID, companyID)
	if err != nil || !ok {
		resp.ErrorLang(c, http.StatusForbidden, "company_not_found_or_access_denied")
		return
	}
	var req CreateDriverInvitationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.DriverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), req.DriverID)
	if err != nil {
		h.logger.Error("driver invitation create check", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if drv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "driver_not_found")
		return
	}
	phone := strings.TrimSpace(drv.Phone)
	if drv.CompanyID != nil && *drv.CompanyID == companyID.String() {
		resp.ErrorLang(c, http.StatusConflict, "driver_already_in_company")
		return
	}
	dup, err := h.repo.HasPendingCompanyInvitation(c.Request.Context(), companyID, phone)
	if err != nil {
		h.logger.Error("driver invitation duplicate check", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if dup {
		resp.ErrorLang(c, http.StatusConflict, "driver_invitation_already_pending")
		return
	}
	token, err := h.repo.Create(c.Request.Context(), companyID, phone, dispatcherID, 7*24*time.Hour)
	if err != nil {
		h.logger.Error("driver invitation create", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if h.stream != nil {
		h.stream.PublishNotification(tripnotif.RecipientDriver, req.DriverID, gin.H{
			"kind":       "connection_offer",
			"event":      "connection_offer_created",
			"direction":  "incoming",
			"type":       "company",
			"token":      token,
			"company_id": companyID.String(),
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"token": token, "expires_in_hours": 168})
}

// CreateForFreelanceReq body for POST /v1/dispatchers/driver-invitations — только driver_id.
type CreateForFreelanceReq struct {
	DriverID uuid.UUID `json:"driver_id" binding:"required"`
}

// CreateForFreelance creates driver invitation as freelance (no company) by driver_id.
func (h *DriverInvitationsHandler) CreateForFreelance(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	disp, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
	if err != nil || disp == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
		return
	}
	role := ""
	if disp.ManagerRole != nil {
		role = strings.TrimSpace(*disp.ManagerRole)
	}
	if role != dispatchers.ManagerRoleDriverManager {
		resp.ErrorLang(c, http.StatusForbidden, "invalid_manager_role")
		return
	}

	count, err := h.drv.GetDriverCount(c.Request.Context(), dispatcherID)
	if err != nil {
		h.logger.Error("get driver count", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if count >= 10 {
		resp.ErrorLang(c, http.StatusConflict, "connection_limit_reached")
		return
	}

	var req CreateForFreelanceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if req.DriverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), req.DriverID)
	if err != nil {
		h.logger.Error("driver invitation create freelance check", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if drv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "driver_not_found")
		return
	}
	phone := strings.TrimSpace(drv.Phone)

	isLinked, _ := h.drv.IsLinked(c.Request.Context(), req.DriverID, dispatcherID)
	if isLinked {
		resp.ErrorLang(c, http.StatusConflict, "driver_already_accepted_your_invitation")
		return
	}

	dup, err := h.repo.HasPendingFreelanceInvitation(c.Request.Context(), dispatcherID, phone)
	if err != nil {
		h.logger.Error("driver invitation duplicate check freelance", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if dup {
		resp.ErrorLang(c, http.StatusConflict, "driver_invitation_already_pending")
		return
	}
	token, err := h.repo.CreateForFreelance(c.Request.Context(), dispatcherID, phone, 7*24*time.Hour)
	if err != nil {
		h.logger.Error("driver invitation create freelance", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if h.stream != nil {
		h.stream.PublishNotification(tripnotif.RecipientDriver, req.DriverID, gin.H{
			"kind":          "connection_offer",
			"event":         "connection_offer_created",
			"direction":     "incoming",
			"type":          "freelance",
			"token":         token,
			"dispatcher_id": dispatcherID.String(),
			"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.SuccessLang(c, http.StatusCreated, "created", gin.H{"token": token, "expires_in_hours": 168})
}

// FindDrivers returns drivers matching phone search (для диспетчера: найти водителя и пригласить по driver_id). Совпадения сверху.
func (h *DriverInvitationsHandler) FindDrivers(c *gin.Context) {
	_ = c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	phoneSearch := strings.TrimSpace(c.Query("phone"))
	if phoneSearch == "" {
		resp.OKLang(c, "ok", gin.H{"items": []gin.H{}})
		return
	}
	limit := 20
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, err := h.drv.SearchByPhone(c.Request.Context(), phoneSearch, limit)
	if err != nil {
		h.logger.Error("drivers find", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_search_drivers")
		return
	}
	if list == nil {
		list = []*drivers.Driver{}
	}
	items := make([]gin.H, 0, len(list))
	for _, d := range list {
		items = append(items, gin.H{
			"id": d.ID, "phone": d.Phone, "name": d.Name,
			"work_status": d.WorkStatus, "driver_type": d.DriverType,
			"freelancer_id": d.FreelancerID, "company_id": d.CompanyID,
		})
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// ListSent returns invitations sent by the current dispatcher (company and freelance). Диспетчер видит кому отправил приглашения.
func (h *DriverInvitationsHandler) ListSent(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	list, err := h.repo.ListByInvitedBy(c.Request.Context(), dispatcherID)
	if err != nil {
		h.logger.Error("driver invitations list sent", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}
	if list == nil {
		list = []driverinvitations.Invitation{}
	}
	items := make([]gin.H, 0, len(list))
	for _, inv := range list {
		st := driverinvitations.EffectiveStatus(inv)
		item := gin.H{
			"token":           inv.Token,
			"phone":           inv.Phone,
			"recipient_phone": inv.Phone,
			"expires_at":      inv.ExpiresAt,
			"created_at":      inv.CreatedAt,
			"status":          st,
			"invited_by":      inv.InvitedBy.String(),
		}
		if inv.RespondedAt != nil {
			item["responded_at"] = inv.RespondedAt
		}
		if inv.CompanyID != nil && *inv.CompanyID != uuid.Nil {
			item["type"] = "company"
			item["company_id"] = inv.CompanyID.String()
		} else {
			item["type"] = "freelance"
			if inv.InvitedByDispatcherID != nil {
				item["dispatcher_id"] = inv.InvitedByDispatcherID.String()
			}
		}
		items = append(items, item)
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// ListConnectionOffers returns one list endpoint for connection offers with direction filter.
// GET /v1/dispatchers/connection-offers?direction=incoming|outgoing|all
func (h *DriverInvitationsHandler) ListConnectionOffers(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	direction := strings.ToLower(strings.TrimSpace(c.DefaultQuery("direction", "all")))
	switch direction {
	case "all", "incoming", "outgoing":
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	items := make([]gin.H, 0)

	if direction == "all" || direction == "outgoing" {
		list, err := h.repo.ListByInvitedBy(c.Request.Context(), dispatcherID)
		if err != nil {
			h.logger.Error("connection offers list outgoing", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
			return
		}
		for _, inv := range list {
			item := gin.H{
				"direction":       "outgoing",
				"token":           inv.Token,
				"phone":           inv.Phone,
				"recipient_phone": inv.Phone,
				"status":          driverinvitations.EffectiveStatus(inv),
				"expires_at":      inv.ExpiresAt,
				"created_at":      inv.CreatedAt,
				"invited_by":      inv.InvitedBy.String(),
			}
			if inv.RespondedAt != nil {
				item["responded_at"] = inv.RespondedAt
			}
			if inv.CompanyID != nil && *inv.CompanyID != uuid.Nil {
				item["type"] = "company"
				item["company_id"] = inv.CompanyID.String()
			} else {
				item["type"] = "freelance"
				if inv.InvitedByDispatcherID != nil {
					item["dispatcher_id"] = inv.InvitedByDispatcherID.String()
				}
			}
			items = append(items, item)
		}
	}

	if direction == "all" || direction == "incoming" {
		if h.disp == nil || h.d2d == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		disp, err := h.disp.FindByID(c.Request.Context(), dispatcherID)
		if err != nil || disp == nil {
			resp.ErrorLang(c, http.StatusUnauthorized, "dispatcher_not_found")
			return
		}
		list, err := h.d2d.ListByDispatcherPhone(c.Request.Context(), disp.Phone)
		if err != nil {
			h.logger.Error("connection offers list incoming", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
			return
		}
		for _, inv := range list {
			item := gin.H{
				"direction":        "incoming",
				"token":            inv.Token,
				"driver_id":        inv.DriverID.String(),
				"dispatcher_phone": inv.DispatcherPhone,
				"from_driver_id":   inv.DriverID.String(),
				"status":           drivertodispatcherinvitations.EffectiveStatus(inv),
				"expires_at":       inv.ExpiresAt,
				"created_at":       inv.CreatedAt,
			}
			if inv.RespondedAt != nil {
				item["responded_at"] = inv.RespondedAt
			}
			drv, _ := h.drv.FindByID(c.Request.Context(), inv.DriverID)
			if drv != nil {
				item["driver_name"] = drv.Name
				item["driver_phone"] = drv.Phone
			}
			items = append(items, item)
		}
	}

	resp.OKLang(c, "ok", gin.H{"direction": direction, "items": items})
}

// ListDriverConnectionOffers returns one list endpoint for driver's connection offers with direction filter.
// GET /v1/driver/connection-offers?direction=incoming|outgoing|all
func (h *DriverInvitationsHandler) ListDriverConnectionOffers(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	direction := strings.ToLower(strings.TrimSpace(c.DefaultQuery("direction", "all")))
	switch direction {
	case "all", "incoming", "outgoing":
	default:
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	items := make([]gin.H, 0)
	currentDriver, _ := h.drv.FindByID(c.Request.Context(), driverID)

	if direction == "all" || direction == "incoming" {
		drv := currentDriver
		if drv == nil {
			var err error
			drv, err = h.drv.FindByID(c.Request.Context(), driverID)
			if err != nil || drv == nil {
				resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
				return
			}
		}
		list, err := h.repo.ListByPhone(c.Request.Context(), drv.Phone)
		if err != nil {
			h.logger.Error("driver connection offers list incoming", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
			return
		}
		for _, inv := range list {
			driverManagerID := inv.InvitedBy.String()
			if inv.InvitedByDispatcherID != nil && *inv.InvitedByDispatcherID != uuid.Nil {
				driverManagerID = inv.InvitedByDispatcherID.String()
			}
			item := gin.H{
				"direction":         "incoming",
				"token":             inv.Token,
				"driver_id":         driverID.String(),
				"driver_phone":      drv.Phone,
				"phone":             inv.Phone,
				"status":            driverinvitations.EffectiveStatus(inv),
				"expires_at":        inv.ExpiresAt,
				"created_at":        inv.CreatedAt,
				"invited_by":        inv.InvitedBy.String(),
				"driver_manager_id": driverManagerID,
			}
			if inv.RespondedAt != nil {
				item["responded_at"] = inv.RespondedAt
			}
			if inv.CompanyID != nil && *inv.CompanyID != uuid.Nil {
				item["type"] = "company"
				item["company_id"] = inv.CompanyID.String()
			} else {
				item["type"] = "freelance"
				if inv.InvitedByDispatcherID != nil {
					item["dispatcher_id"] = inv.InvitedByDispatcherID.String()
				}
			}
			if disp, err := h.disp.FindByID(c.Request.Context(), inv.InvitedBy); err == nil && disp != nil {
				item["driver_manager_name"] = disp.Name
				item["driver_manager_phone"] = disp.Phone
				item["driver_manager_has_photo"] = disp.HasPhoto
				if disp.HasPhoto {
					item["driver_manager_photo_url"] = "/v1/chat/users/" + disp.ID + "/photo"
				}
			}
			items = append(items, item)
		}
	}

	if direction == "all" || direction == "outgoing" {
		if h.d2d == nil {
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		list, err := h.d2d.ListByDriverID(c.Request.Context(), driverID)
		if err != nil {
			h.logger.Error("driver connection offers list outgoing", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
			return
		}
		for _, inv := range list {
			item := gin.H{
				"direction":           "outgoing",
				"token":               inv.Token,
				"dispatcher_phone":    inv.DispatcherPhone,
				"to_dispatcher_phone": inv.DispatcherPhone,
				"driver_id":           inv.DriverID.String(),
				"status":              drivertodispatcherinvitations.EffectiveStatus(inv),
				"expires_at":          inv.ExpiresAt,
				"created_at":          inv.CreatedAt,
			}
			if currentDriver != nil {
				item["driver_phone"] = currentDriver.Phone
			}
			if inv.RespondedAt != nil {
				item["responded_at"] = inv.RespondedAt
			}
			if disp, err := h.disp.FindByPhone(c.Request.Context(), inv.DispatcherPhone); err == nil && disp != nil {
				item["driver_manager_id"] = disp.ID
				item["driver_manager_name"] = disp.Name
				item["driver_manager_phone"] = disp.Phone
				item["driver_manager_has_photo"] = disp.HasPhoto
				if disp.HasPhoto {
					item["driver_manager_photo_url"] = "/v1/chat/users/" + disp.ID + "/photo"
				}
			}
			items = append(items, item)
		}
	}

	resp.OKLang(c, "ok", gin.H{"direction": direction, "items": items})
}

// GetMyDriver returns one linked driver by ID for current dispatcher.
// GET /v1/dispatchers/drivers/:driverId
func (h *DriverInvitationsHandler) GetMyDriver(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}
	if drv.FreelancerID == nil || *drv.FreelancerID != dispatcherID.String() {
		resp.ErrorLang(c, http.StatusForbidden, "driver_not_linked")
		return
	}
	resp.OKLang(c, "ok", gin.H{"driver": drv})
}

// UnlinkDriver removes driver from dispatcher's list (sets driver.freelancer_id = NULL). Водитель должен быть принят по приглашению (freelancer_id = я).
func (h *DriverInvitationsHandler) UnlinkDriver(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	ok, err := h.drv.UnlinkFromFreelancer(c.Request.Context(), driverID, dispatcherID)
	if err != nil {
		h.logger.Error("unlink driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_unlink")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusForbidden, "driver_not_linked")
		return
	}
	resp.OKLang(c, "ok", nil)
}

// SetDriverPowerReq body for PUT /v1/dispatchers/drivers/:driverId/power
type SetDriverPowerReq struct {
	PowerPlateType   *string `json:"power_plate_type,omitempty"`
	PowerPlateNumber *string `json:"power_plate_number,omitempty"`
	PowerTechSeries  *string `json:"power_tech_series,omitempty"`
	PowerTechNumber  *string `json:"power_tech_number,omitempty"`
	PowerOwnerName   *string `json:"power_owner_name,omitempty"`
	PowerScanStatus  *bool   `json:"power_scan_status,omitempty"`
}

// SetDriverPower adds or updates тягач for a driver. Водитель должен быть принят по приглашению (freelancer_id = я).
func (h *DriverInvitationsHandler) SetDriverPower(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}
	if drv.FreelancerID == nil || *drv.FreelancerID != dispatcherID.String() {
		resp.ErrorLang(c, http.StatusForbidden, "driver_must_accept_invitation")
		return
	}
	var req SetDriverPowerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	trimPtr := func(p **string) {
		if p == nil || *p == nil {
			return
		}
		v := strings.TrimSpace(**p)
		if v == "" {
			*p = nil
			return
		}
		*p = &v
	}
	trimPtr(&req.PowerPlateType)
	trimPtr(&req.PowerPlateNumber)
	trimPtr(&req.PowerTechSeries)
	trimPtr(&req.PowerTechNumber)
	trimPtr(&req.PowerOwnerName)
	// Validate power_plate_type when provided.
	if req.PowerPlateType != nil {
		v := strings.ToUpper(strings.TrimSpace(*req.PowerPlateType))
		if v == "" {
			req.PowerPlateType = nil
		} else {
			if v != "TRUCK" && v != "TRACTOR" {
				resp.ErrorLang(c, http.StatusBadRequest, "invalid_power_plate_type")
				return
			}
			req.PowerPlateType = &v
		}
	}
	if err := h.drv.UpdatePowerProfile(c.Request.Context(), driverID, drivers.UpdatePowerProfile{
		PowerPlateType:   req.PowerPlateType,
		PowerPlateNumber: req.PowerPlateNumber,
		PowerTechSeries:  req.PowerTechSeries,
		PowerTechNumber:  req.PowerTechNumber,
		PowerOwnerName:   req.PowerOwnerName,
		PowerScanStatus:  req.PowerScanStatus,
	}); err != nil {
		h.logger.Error("dispatcher set driver power", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update_power")
		return
	}
	updated, _ := h.drv.FindByID(c.Request.Context(), driverID)
	publishDriverUpdateToManager(h.stream, h.logger, updated, "dispatcher", dispatcherID.String(), "dispatcher.driver.power.put", []string{
		"power_plate_type", "power_plate_number", "power_tech_series", "power_tech_number", "power_owner_name", "power_scan_status",
	})
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": updated})
}

// SetDriverTrailerReq body for PUT /v1/dispatchers/drivers/:driverId/trailer
type SetDriverTrailerReq struct {
	TrailerPlateType   *string `json:"trailer_plate_type,omitempty"`
	TrailerPlateNumber *string `json:"trailer_plate_number,omitempty"`
	TrailerTechSeries  *string `json:"trailer_tech_series,omitempty"`
	TrailerTechNumber  *string `json:"trailer_tech_number,omitempty"`
	TrailerOwnerName   *string `json:"trailer_owner_name,omitempty"`
	TrailerScanStatus  *bool   `json:"trailer_scan_status,omitempty"`
}

// SetDriverTrailer adds or updates прицеп for a driver. Водитель должен быть принят по приглашению (freelancer_id = я).
func (h *DriverInvitationsHandler) SetDriverTrailer(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}
	if drv.FreelancerID == nil || *drv.FreelancerID != dispatcherID.String() {
		resp.ErrorLang(c, http.StatusForbidden, "driver_must_accept_invitation")
		return
	}
	var req SetDriverTrailerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	trimPtr := func(p **string) {
		if p == nil || *p == nil {
			return
		}
		v := strings.TrimSpace(**p)
		if v == "" {
			*p = nil
			return
		}
		*p = &v
	}
	trimPtr(&req.TrailerPlateType)
	trimPtr(&req.TrailerPlateNumber)
	trimPtr(&req.TrailerTechSeries)
	trimPtr(&req.TrailerTechNumber)
	trimPtr(&req.TrailerOwnerName)
	// Validate trailer_plate_type when provided (against power_plate_type if we know it).
	if req.TrailerPlateType != nil {
		v := strings.ToUpper(strings.TrimSpace(*req.TrailerPlateType))
		if v == "" {
			req.TrailerPlateType = nil
		} else {
			power := ""
			if drv.PowerPlateType != nil {
				power = *drv.PowerPlateType
			}
			if power != "" {
				if errKey := validatePowerTrailerTypes(power, v); errKey != "" {
					resp.ErrorLang(c, http.StatusBadRequest, errKey)
					return
				}
			}
			req.TrailerPlateType = &v
		}
	}
	if err := h.drv.UpdateTrailerProfile(c.Request.Context(), driverID, drivers.UpdateTrailerProfile{
		TrailerPlateType:   req.TrailerPlateType,
		TrailerPlateNumber: req.TrailerPlateNumber,
		TrailerTechSeries:  req.TrailerTechSeries,
		TrailerTechNumber:  req.TrailerTechNumber,
		TrailerOwnerName:   req.TrailerOwnerName,
		TrailerScanStatus:  req.TrailerScanStatus,
	}); err != nil {
		h.logger.Error("dispatcher set driver trailer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update_trailer")
		return
	}
	updated, _ := h.drv.FindByID(c.Request.Context(), driverID)
	publishDriverUpdateToManager(h.stream, h.logger, updated, "dispatcher", dispatcherID.String(), "dispatcher.driver.trailer.put", []string{
		"trailer_plate_type", "trailer_plate_number", "trailer_tech_series", "trailer_tech_number", "trailer_owner_name", "trailer_scan_status",
	})
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": updated})
}

type PatchMyDriverReq struct {
	WorkStatus           *string `json:"work_status,omitempty"`
	DriverPassportSeries *string `json:"driver_passport_series,omitempty"`
	DriverPassportNumber *string `json:"driver_passport_number,omitempty"`
	DriverPINFL          *string `json:"driver_pinfl,omitempty"`
	DriverScanStatus     *bool   `json:"driver_scan_status,omitempty"`
	DriverType           *string `json:"driver_type,omitempty"`    // company|freelancer|driver
	AccountStatus        *string `json:"account_status,omitempty"` // active|... (project-defined)
	DriverOwner          *bool   `json:"driver_owner,omitempty"`
	KYCStatus            *string `json:"kyc_status,omitempty"`          // pending|approved|...
	RegistrationStep     *string `json:"registration_step,omitempty"`   // optional
	RegistrationStatus   *string `json:"registration_status,omitempty"` // START|BASIC|FULL

	PowerPlateType   *string `json:"power_plate_type,omitempty"`
	PowerPlateNumber *string `json:"power_plate_number,omitempty"`
	PowerTechSeries  *string `json:"power_tech_series,omitempty"`
	PowerTechNumber  *string `json:"power_tech_number,omitempty"`
	PowerOwnerName   *string `json:"power_owner_name,omitempty"`
	PowerScanStatus  *bool   `json:"power_scan_status,omitempty"`

	TrailerPlateType   *string `json:"trailer_plate_type,omitempty"`
	TrailerPlateNumber *string `json:"trailer_plate_number,omitempty"`
	TrailerTechSeries  *string `json:"trailer_tech_series,omitempty"`
	TrailerTechNumber  *string `json:"trailer_tech_number,omitempty"`
	TrailerOwnerName   *string `json:"trailer_owner_name,omitempty"`
	TrailerScanStatus  *bool   `json:"trailer_scan_status,omitempty"`
}

// PatchMyDriver allows dispatcher to update linked driver fields (except driver name/phone).
// PATCH /v1/dispatchers/drivers/:driverId
func (h *DriverInvitationsHandler) PatchMyDriver(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	driverID, err := uuid.Parse(c.Param("driverId"))
	if err != nil || driverID == uuid.Nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_id")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "driver_not_found")
		return
	}
	if drv.FreelancerID == nil || *drv.FreelancerID != dispatcherID.String() {
		resp.ErrorLang(c, http.StatusForbidden, "driver_must_accept_invitation")
		return
	}

	var req PatchMyDriverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	trimPtr := func(p **string) {
		if p == nil || *p == nil {
			return
		}
		v := strings.TrimSpace(**p)
		if v == "" {
			*p = nil
			return
		}
		*p = &v
	}
	trimPtr(&req.WorkStatus)
	trimPtr(&req.DriverPassportSeries)
	trimPtr(&req.DriverPassportNumber)
	trimPtr(&req.DriverPINFL)
	trimPtr(&req.DriverType)
	trimPtr(&req.AccountStatus)
	trimPtr(&req.KYCStatus)
	trimPtr(&req.RegistrationStep)
	trimPtr(&req.RegistrationStatus)
	trimPtr(&req.PowerPlateType)
	trimPtr(&req.PowerPlateNumber)
	trimPtr(&req.PowerTechSeries)
	trimPtr(&req.PowerTechNumber)
	trimPtr(&req.PowerOwnerName)
	trimPtr(&req.TrailerPlateType)
	trimPtr(&req.TrailerPlateNumber)
	trimPtr(&req.TrailerTechSeries)
	trimPtr(&req.TrailerTechNumber)
	trimPtr(&req.TrailerOwnerName)

	if req.WorkStatus != nil {
		v := strings.ToLower(*req.WorkStatus)
		switch v {
		case "available", "loaded", "busy":
			req.WorkStatus = &v
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_work_status")
			return
		}
	}
	if req.DriverPassportSeries != nil {
		if errKey := validatePassportSeries(*req.DriverPassportSeries); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
	}
	if req.DriverPassportNumber != nil {
		if errKey := validatePassportNumber(*req.DriverPassportNumber); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
	}
	if req.DriverPINFL != nil {
		if errKey := validatePINFL(*req.DriverPINFL); errKey != "" {
			resp.ErrorLang(c, http.StatusBadRequest, errKey)
			return
		}
	}
	if req.DriverType != nil {
		v := strings.ToLower(*req.DriverType)
		switch domain.DriverType(v) {
		case domain.DriverTypeCompany, domain.DriverTypeFreelancer, domain.DriverTypeDriver:
			req.DriverType = &v
		default:
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_driver_type")
			return
		}
	}
	if req.PowerPlateType != nil {
		v := strings.ToUpper(*req.PowerPlateType)
		if v != "TRUCK" && v != "TRACTOR" {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_power_plate_type")
			return
		}
		req.PowerPlateType = &v
	}
	if req.TrailerPlateType != nil {
		v := strings.ToUpper(*req.TrailerPlateType)
		power := ""
		if req.PowerPlateType != nil {
			power = *req.PowerPlateType
		} else if drv.PowerPlateType != nil {
			power = *drv.PowerPlateType
		}
		if power != "" {
			if errKey := validatePowerTrailerTypes(power, v); errKey != "" {
				resp.ErrorLang(c, http.StatusBadRequest, errKey)
				return
			}
		}
		req.TrailerPlateType = &v
	}
	if req.RegistrationStatus != nil {
		v := strings.ToUpper(*req.RegistrationStatus)
		req.RegistrationStatus = &v
	}

	if err := h.drv.UpdateDriverByDispatcher(c.Request.Context(), driverID, drivers.UpdateDriverByDispatcher{
		WorkStatus:           req.WorkStatus,
		DriverPassportSeries: req.DriverPassportSeries,
		DriverPassportNumber: req.DriverPassportNumber,
		DriverPINFL:          req.DriverPINFL,
		DriverScanStatus:     req.DriverScanStatus,
		DriverType:           req.DriverType,
		AccountStatus:        req.AccountStatus,
		DriverOwner:          req.DriverOwner,
		KYCStatus:            req.KYCStatus,
		RegistrationStep:     req.RegistrationStep,
		RegistrationStatus:   req.RegistrationStatus,
	}); err != nil {
		h.logger.Error("dispatcher patch my driver", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := h.drv.UpdatePowerProfile(c.Request.Context(), driverID, drivers.UpdatePowerProfile{
		PowerPlateType:   req.PowerPlateType,
		PowerPlateNumber: req.PowerPlateNumber,
		PowerTechSeries:  req.PowerTechSeries,
		PowerTechNumber:  req.PowerTechNumber,
		PowerOwnerName:   req.PowerOwnerName,
		PowerScanStatus:  req.PowerScanStatus,
	}); err != nil {
		h.logger.Error("dispatcher patch my driver power", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update_power")
		return
	}
	if err := h.drv.UpdateTrailerProfile(c.Request.Context(), driverID, drivers.UpdateTrailerProfile{
		TrailerPlateType:   req.TrailerPlateType,
		TrailerPlateNumber: req.TrailerPlateNumber,
		TrailerTechSeries:  req.TrailerTechSeries,
		TrailerTechNumber:  req.TrailerTechNumber,
		TrailerOwnerName:   req.TrailerOwnerName,
		TrailerScanStatus:  req.TrailerScanStatus,
	}); err != nil {
		h.logger.Error("dispatcher patch my driver trailer", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_update_trailer")
		return
	}

	updated, _ := h.drv.FindByID(c.Request.Context(), driverID)
	changed := make([]string, 0, 24)
	addChanged := func(name string, set bool) {
		if set {
			changed = append(changed, name)
		}
	}
	addChanged("work_status", req.WorkStatus != nil)
	addChanged("driver_passport_series", req.DriverPassportSeries != nil)
	addChanged("driver_passport_number", req.DriverPassportNumber != nil)
	addChanged("driver_pinfl", req.DriverPINFL != nil)
	addChanged("driver_scan_status", req.DriverScanStatus != nil)
	addChanged("driver_type", req.DriverType != nil)
	addChanged("account_status", req.AccountStatus != nil)
	addChanged("driver_owner", req.DriverOwner != nil)
	addChanged("kyc_status", req.KYCStatus != nil)
	addChanged("registration_step", req.RegistrationStep != nil)
	addChanged("registration_status", req.RegistrationStatus != nil)
	addChanged("power_plate_type", req.PowerPlateType != nil)
	addChanged("power_plate_number", req.PowerPlateNumber != nil)
	addChanged("power_tech_series", req.PowerTechSeries != nil)
	addChanged("power_tech_number", req.PowerTechNumber != nil)
	addChanged("power_owner_name", req.PowerOwnerName != nil)
	addChanged("power_scan_status", req.PowerScanStatus != nil)
	addChanged("trailer_plate_type", req.TrailerPlateType != nil)
	addChanged("trailer_plate_number", req.TrailerPlateNumber != nil)
	addChanged("trailer_tech_series", req.TrailerTechSeries != nil)
	addChanged("trailer_tech_number", req.TrailerTechNumber != nil)
	addChanged("trailer_owner_name", req.TrailerOwnerName != nil)
	addChanged("trailer_scan_status", req.TrailerScanStatus != nil)
	publishDriverUpdateToManager(h.stream, h.logger, updated, "dispatcher", dispatcherID.String(), "dispatcher.driver.patch", changed)
	resp.OKLang(c, "updated", gin.H{"event": "updated", "driver": updated})
}

// CancelInvitation cancels (revokes) an invitation sent by the current dispatcher. Только свои приглашения.
func (h *DriverInvitationsHandler) CancelInvitation(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		resp.ErrorLang(c, http.StatusBadRequest, "token_required")
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_required")
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 3 {
		resp.ErrorLang(c, http.StatusBadRequest, "rejection_reason_too_short")
		return
	}
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), token)
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusNotFound, "invitation_not_found_or_expired")
		return
	}
	if inv.InvitedBy != dispatcherID {
		resp.ErrorLang(c, http.StatusForbidden, "not_your_invitation")
		return
	}
	ok, err := h.repo.DeletePendingByToken(c.Request.Context(), token)
	if err != nil {
		h.logger.Error("driver invitation cancel", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_cancel_invitation")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusNotFound, "invitation_not_found_or_expired")
		return
	}
	if h.stream != nil {
		drvList, _ := h.drv.SearchByPhone(c.Request.Context(), inv.Phone, 1)
		if len(drvList) > 0 && drvList[0] != nil {
			if driverUUID, err := uuid.Parse(drvList[0].ID); err == nil && driverUUID != uuid.Nil {
				h.stream.PublishNotification(tripnotif.RecipientDriver, driverUUID, gin.H{
					"kind":       "connection_offer",
					"event":      "connection_offer_cancelled",
					"direction":  "incoming",
					"token":      token,
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
		}
	}
	resp.OKLang(c, "ok", nil)
}

// ListInvitations returns pending invitations for the current driver (by phone). Водитель видит приглашения в чате/разделе приглашений.
func (h *DriverInvitationsHandler) ListInvitations(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	list, err := h.repo.ListByPhone(c.Request.Context(), drv.Phone)
	if err != nil {
		h.logger.Error("driver invitations list", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}
	if list == nil {
		list = []driverinvitations.Invitation{}
	}
	items := make([]gin.H, 0, len(list))
	for _, inv := range list {
		st := driverinvitations.EffectiveStatus(inv)
		item := gin.H{
			"token":      inv.Token,
			"phone":      inv.Phone,
			"expires_at": inv.ExpiresAt,
			"created_at": inv.CreatedAt,
			"status":     st,
			"invited_by": inv.InvitedBy.String(),
		}
		if inv.RespondedAt != nil {
			item["responded_at"] = inv.RespondedAt
		}
		if inv.CompanyID != nil && *inv.CompanyID != uuid.Nil {
			item["type"] = "company"
			item["company_id"] = inv.CompanyID.String()
		} else if inv.InvitedByDispatcherID != nil && *inv.InvitedByDispatcherID != uuid.Nil {
			item["type"] = "freelance"
			item["dispatcher_id"] = inv.InvitedByDispatcherID.String()
		} else {
			item["type"] = "unknown"
		}
		items = append(items, item)
	}
	resp.OKLang(c, "ok", gin.H{"items": items})
}

// AcceptDriverInvitationReq body for POST /v1/driver/driver-invitations/accept
type AcceptDriverInvitationReq struct {
	Token string `json:"token" binding:"required"`
}

// AcceptDriverInvitation links driver to company or to freelance dispatcher (driver's phone must match invitation).
func (h *DriverInvitationsHandler) Accept(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req AcceptDriverInvitationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), strings.TrimSpace(req.Token))
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	if strings.TrimSpace(strings.ReplaceAll(inv.Phone, " ", "")) != strings.TrimSpace(strings.ReplaceAll(drv.Phone, " ", "")) {
		resp.ErrorLang(c, http.StatusForbidden, "invitation_sent_to_another_phone")
		return
	}
	token := strings.TrimSpace(req.Token)
	if inv.CompanyID != nil && *inv.CompanyID != uuid.Nil {
		ok, err := h.repo.SetStatusIfPending(c.Request.Context(), token, driverinvitations.StatusAccepted)
		if err != nil || !ok {
			resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
			return
		}
		if err := h.drv.SetCompanyID(c.Request.Context(), driverID, *inv.CompanyID); err != nil {
			_ = h.repo.RevertToPending(c.Request.Context(), token)
			h.logger.Error("driver set company", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_accept")
			return
		}
		if h.stream != nil {
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, inv.InvitedBy, gin.H{
				"kind":       "connection_offer",
				"event":      "connection_offer_accepted",
				"direction":  "outgoing",
				"type":       "company",
				"token":      token,
				"driver_id":  driverID.String(),
				"company_id": inv.CompanyID.String(),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		resp.SuccessLang(c, http.StatusOK, "accepted", gin.H{"company_id": inv.CompanyID.String()})
		return
	}
	if inv.InvitedByDispatcherID != nil && *inv.InvitedByDispatcherID != uuid.Nil {
		// Many-to-Many Connection Limit Check
		dCount, err := h.drv.GetDriverCount(c.Request.Context(), *inv.InvitedByDispatcherID)
		if err != nil {
			h.logger.Error("get manager driver count", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if dCount >= 10 {
			resp.ErrorLang(c, http.StatusConflict, "connection_limit_reached")
			return
		}

		mCount, err := h.drv.GetManagerCount(c.Request.Context(), driverID)
		if err != nil {
			h.logger.Error("get driver manager count", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
			return
		}
		if mCount >= 10 {
			resp.ErrorLang(c, http.StatusConflict, "connection_limit_reached")
			return
		}

		ok, err := h.repo.SetStatusIfPending(c.Request.Context(), token, driverinvitations.StatusAccepted)
		if err != nil || !ok {
			resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
			return
		}

		// Use the new many-to-many link
		if err := h.drv.LinkManager(c.Request.Context(), driverID, *inv.InvitedByDispatcherID); err != nil {
			_ = h.repo.RevertToPending(c.Request.Context(), token)
			h.logger.Error("driver link manager", zap.Error(err))
			resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_accept")
			return
		}

		// Backward compatibility: set primary freelancer_id if not set
		if drv.FreelancerID == nil || *drv.FreelancerID == "" {
			_ = h.drv.SetFreelancerID(c.Request.Context(), driverID, *inv.InvitedByDispatcherID)
		}

		if h.stream != nil {
			h.stream.PublishNotification(tripnotif.RecipientDispatcher, *inv.InvitedByDispatcherID, gin.H{
				"kind":          "connection_offer",
				"event":         "connection_offer_accepted",
				"direction":     "outgoing",
				"type":          "freelance",
				"token":         token,
				"driver_id":     driverID.String(),
				"dispatcher_id": inv.InvitedByDispatcherID.String(),
				"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		resp.SuccessLang(c, http.StatusOK, "accepted", gin.H{"freelancer_id": inv.InvitedByDispatcherID.String()})
		return
	}
	resp.ErrorLang(c, http.StatusBadRequest, "invitation_invalid")
}

// DeclineDriverInvitationReq body for POST /v1/driver/driver-invitations/decline
type DeclineDriverInvitationReq struct {
	Token string `json:"token" binding:"required"`
}

// DeclineDriverInvitation удаляет приглашение (водитель отказывается). Проверяем, что приглашение было на этот номер.
func (h *DriverInvitationsHandler) Decline(c *gin.Context) {
	driverID := c.MustGet(mw.CtxDriverID).(uuid.UUID)
	var req DeclineDriverInvitationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	token := strings.TrimSpace(req.Token)
	inv, err := h.repo.GetPendingByToken(c.Request.Context(), token)
	if err != nil || inv == nil {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	drv, err := h.drv.FindByID(c.Request.Context(), driverID)
	if err != nil || drv == nil {
		resp.ErrorLang(c, http.StatusUnauthorized, "driver_not_found")
		return
	}
	if strings.TrimSpace(strings.ReplaceAll(inv.Phone, " ", "")) != strings.TrimSpace(strings.ReplaceAll(drv.Phone, " ", "")) {
		resp.ErrorLang(c, http.StatusForbidden, "invitation_sent_to_another_phone")
		return
	}
	ok, err := h.repo.SetStatusIfPending(c.Request.Context(), token, driverinvitations.StatusDeclined)
	if err != nil {
		h.logger.Error("driver invitation decline", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "internal_error")
		return
	}
	if !ok {
		resp.ErrorLang(c, http.StatusBadRequest, "invitation_not_found_or_expired")
		return
	}
	if h.stream != nil {
		target := inv.InvitedBy
		if inv.InvitedByDispatcherID != nil && *inv.InvitedByDispatcherID != uuid.Nil {
			target = *inv.InvitedByDispatcherID
		}
		h.stream.PublishNotification(tripnotif.RecipientDispatcher, target, gin.H{
			"kind":       "connection_offer",
			"event":      "connection_offer_declined",
			"direction":  "outgoing",
			"token":      token,
			"driver_id":  driverID.String(),
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	resp.OKLang(c, "declined", gin.H{"status": "declined"})
}

// ListMyDrivers returns drivers linked to the current freelance dispatcher (freelancer_id = me) with filters and pagination.
// Query: phone (search), work_status, truck_type (power_plate_type), page, limit, sort (e.g. updated_at:desc, name:asc, last_online_at:desc).
func (h *DriverInvitationsHandler) ListMyDrivers(c *gin.Context) {
	dispatcherID := c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	f := drivers.ListDriversFilter{
		Phone:      strings.TrimSpace(c.Query("phone")),
		WorkStatus: strings.TrimSpace(c.Query("work_status")),
		TruckType:  strings.TrimSpace(c.Query("truck_type")),
		Page:       1,
		Limit:      20,
		Sort:       strings.TrimSpace(c.DefaultQuery("sort", "updated_at:desc")),
	}
	if p := c.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			f.Page = n
		}
	}
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			f.Limit = n
		}
	}
	list, total, err := h.drv.ListByFreelancerIDFilter(c.Request.Context(), dispatcherID, f)
	if err != nil {
		h.logger.Error("list my drivers", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_drivers")
		return
	}
	if list == nil {
		list = []*drivers.Driver{}
	}
	resp.OKLang(c, "ok", gin.H{"items": list, "total": total})
}

// ListAllDriversForFreelance returns all drivers in the system (not only linked ones) with filters and pagination.
// Query: phone (search), work_status, truck_type (power_plate_type), page, limit, sort (e.g. updated_at:desc, name:asc, last_online_at:desc, work_status:asc).
func (h *DriverInvitationsHandler) ListAllDriversForFreelance(c *gin.Context) {
	_ = c.MustGet(mw.CtxDispatcherID).(uuid.UUID)
	var (
		latPtr    *float64
		lngPtr    *float64
		radiusPtr *float64
		hasPhoto  *bool
	)
	if v := strings.TrimSpace(c.Query("latitude")); v != "" {
		if f64, err := strconv.ParseFloat(v, 64); err == nil {
			latPtr = &f64
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_latitude")
			return
		}
	}
	if v := strings.TrimSpace(c.Query("longitude")); v != "" {
		if f64, err := strconv.ParseFloat(v, 64); err == nil {
			lngPtr = &f64
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_longitude")
			return
		}
	}
	if v := strings.TrimSpace(c.Query("radius_km")); v != "" {
		if f64, err := strconv.ParseFloat(v, 64); err == nil && f64 > 0 {
			radiusPtr = &f64
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
	}
	if (latPtr != nil || lngPtr != nil || radiusPtr != nil) && !(latPtr != nil && lngPtr != nil && radiusPtr != nil) {
		resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
		return
	}
	if v := strings.TrimSpace(c.Query("has_photo")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			hasPhoto = &b
		} else {
			resp.ErrorLang(c, http.StatusBadRequest, "invalid_payload_detail")
			return
		}
	}
	f := drivers.ListDriversFilter{
		Phone:         strings.TrimSpace(c.Query("phone")),
		Name:          strings.TrimSpace(c.Query("name")),
		WorkStatus:    strings.TrimSpace(c.Query("work_status")),
		TruckType:     strings.TrimSpace(c.Query("truck_type")),
		DriverType:    strings.TrimSpace(c.Query("driver_type")),
		AccountStatus: strings.TrimSpace(c.Query("account_status")),
		HasPhoto:      hasPhoto,
		Latitude:      latPtr,
		Longitude:     lngPtr,
		RadiusKM:      radiusPtr,
		Page:          1,
		Limit:         20,
		Sort:          strings.TrimSpace(c.DefaultQuery("sort", "updated_at:desc")),
	}
	if p := c.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			f.Page = n
		}
	}
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			f.Limit = n
		}
	}
	list, total, err := h.drv.ListAllForFreelancerFilter(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("list all drivers for freelance dispatcher", zap.Error(err))
		resp.ErrorLang(c, http.StatusInternalServerError, "failed_to_list_drivers")
		return
	}
	if list == nil {
		list = []*drivers.Driver{}
	}
	totalPages := 0
	if f.Limit > 0 {
		totalPages = (total + f.Limit - 1) / f.Limit
	}
	resp.OKLang(c, "ok", gin.H{
		"items":       list,
		"total":       total,
		"page":        f.Page,
		"limit":       f.Limit,
		"total_pages": totalPages,
		"has_next":    f.Page < totalPages,
	})
}

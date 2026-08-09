package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/bus"
)

// GET /api/me/buses/{bus_id}/credentials · Credential[] · 前端 TS 契约
// GET /api/me/buses/{bus_id}/pulls · PullRound[]

type credDTO struct {
	ID              string  `json:"id"`
	VendorID        string  `json:"vendor_id"`
	Status          string  `json:"status"` // alive | dead
	KeyMasked       string  `json:"key_masked"`
	Account         string  `json:"account"`
	Region          string  `json:"region"`
	IssuerURL       string  `json:"issuer_url"`
	CreditsUsed     int64   `json:"credits_used"`
	PulledAt        string  `json:"pulled_at"`
	WarrantyUntil   *string `json:"warranty_until"`
	DeadAt          *string `json:"dead_at"`
	LifespanSeconds int64   `json:"lifespan_seconds"`
	Paid            int64   `json:"paid"`
	OwnerBusID      *string `json:"owner_bus_id"`
	PushedAt        *string `json:"pushed_at"`
	PushFailed      bool    `json:"push_failed"`
	PushError       any     `json:"push_error"`
}

func (s *Server) handleBusCredentials(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, vendor_id, status,
		       COALESCE(pulled_at, ''), warranty_until, dead_at
		  FROM credential_ledger
		 WHERE owner_bus_id = ?
		 ORDER BY pulled_at DESC`, busID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]credDTO, 0)
	for rows.Next() {
		var c credDTO
		var warranty, deadAt sql.NullString
		if err := rows.Scan(&c.ID, &c.VendorID, &c.Status, &c.PulledAt, &warranty, &deadAt); err != nil {
			return err
		}
		if warranty.Valid {
			s := warranty.String
			c.WarrantyUntil = &s
		}
		if deadAt.Valid {
			s := deadAt.String
			c.DeadAt = &s
		}
		bid := busID
		c.OwnerBusID = &bid
		// 明文 / 派发瞬时快照 1a 不接 · 空字段前端已能处理
		c.KeyMasked = "ksk_" + shortID(c.ID) + "…" + tailID(c.ID)
		items = append(items, c)
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

type busPullDTO struct {
	ID             string  `json:"id"`
	VendorID       string  `json:"vendor_id"`
	BusID          *string `json:"bus_id"`
	BusName        *string `json:"bus_name"`
	Result         string  `json:"result"`
	CountRequested int     `json:"count_requested"`
	CountPurchased int     `json:"count_purchased"`
	AliveCount     int     `json:"alive_count"`
	DeadCount      int     `json:"dead_count"`
	PushState      string  `json:"push_state"`
	PushRatio      *string `json:"push_ratio"`
	TotalCost      int64   `json:"total_cost"`
	FailReason     *string `json:"fail_reason"`
	CreatedAt      string  `json:"created_at"`
}

func (s *Server) handleBusPulls(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT pr.id, pr.vendor_id, pr.bus_id, pr.count_requested, pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status, pr.created_at,
		       (SELECT COUNT(1) FROM credential_ledger WHERE source_pull_round_id = pr.id AND status = 'alive'),
		       (SELECT COUNT(1) FROM credential_ledger WHERE source_pull_round_id = pr.id AND status = 'dead')
		  FROM pull_round pr
		 WHERE pr.bus_id = ?
		 ORDER BY pr.created_at DESC`, busID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]busPullDTO, 0)
	for rows.Next() {
		var p busPullDTO
		var busIDCol sql.NullString
		var internalStatus string
		var totalCost int64
		if err := rows.Scan(&p.ID, &p.VendorID, &busIDCol,
			&p.CountRequested, &p.CountPurchased, &totalCost,
			&internalStatus, &p.CreatedAt, &p.AliveCount, &p.DeadCount); err != nil {
			return err
		}
		if busIDCol.Valid {
			s := busIDCol.String
			p.BusID = &s
		}
		p.TotalCost = -totalCost
		p.Result = mapPullRoundResult(internalStatus, p.CountRequested, p.CountPurchased)
		p.PushState = "none" // 阶段 1a 不做推 passengerpool
		items = append(items, p)
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

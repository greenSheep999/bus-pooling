package kiro91

// wire types — vendor 私有，不外暴。

type profileResp struct {
	Profile struct {
		Balance          int64 `json:"balance"`
		Spent            int64 `json:"spent"`
		Earned           int64 `json:"earned"`
		MaxKeysHeld      int   `json:"max_keys_held"`
		HoldCapEffective int   `json:"hold_cap_effective"`
		KeysHeld         int   `json:"keys_held"`
	} `json:"profile"`
}

type stockResp struct {
	Stock struct {
		PublicAvailable int `json:"public_available"`
		MyPrivate       int `json:"my_private"`
	} `json:"stock"`
	Zones []zoneItem `json:"zones"`
	Max   int        `json:"max"`
	Min   int        `json:"min_per_order"`
	MaxPO int        `json:"max_per_order"`
	WM    int        `json:"warranty_minutes"`
}

type zoneItem struct {
	Zone      string `json:"zone"`
	Region    string `json:"region"`
	Available int    `json:"available"`
	UnitPrice int64  `json:"unit_price"`
}

type purchaseReq struct {
	Count         int    `json:"count"`
	Zone          string `json:"zone,omitempty"`
	ClientOrderID string `json:"client_order_id"`
}

type purchaseResp struct {
	ClientOrderID string    `json:"client_order_id"`
	OrderID       string    `json:"order_id"`
	Zone          string    `json:"zone"`
	Purchased     int       `json:"purchased"`
	UnitPrice     int64     `json:"unit_price"`
	TotalCredits  int64     `json:"total_credits"`
	Remaining     int64     `json:"remaining"`
	Keys          []keyItem `json:"keys"`
	WarrantyUntil string    `json:"warranty_until"`
	WM            int       `json:"warranty_minutes"`
}

type keyItem struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Account       string `json:"account"`
	Password      string `json:"password"`
	IssuerURL     string `json:"issuer_url"`
	Free          bool   `json:"free"`
	Paid          int64  `json:"paid"`
	WarrantyUntil string `json:"warranty_until"`
}

type redeemReq struct {
	Code string `json:"code"`
}

type redeemResp struct {
	Quota   int64 `json:"quota"`
	Balance int64 `json:"balance"`
}

type usageResp struct {
	Usage struct {
		Remaining int64 `json:"remaining"`
		Total     int64 `json:"total"`
		Synced    int   `json:"synced"`
		Keys      int   `json:"keys"`
	} `json:"usage"`
}

type errorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (e *errorResp) msg() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Error
}

type webhookPayload struct {
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	Visibility      string `json:"visibility"`
	NewKeys         int    `json:"new_keys"`
	Dead            int    `json:"dead"`
	Zone            string `json:"zone"`
	OrderID         string `json:"order_id"`
	PurchaseOrderID string `json:"purchase_order_id"`
	PoolID          string `json:"pool_id"`
	RoundID         string `json:"round_id"`
	MotherID        string `json:"mother_id"`
	RefundedQuota   int64  `json:"refunded_quota"`
	RefundedKeys    int    `json:"refunded_keys"`
	Reason          string `json:"reason"`
	Timestamp       int64  `json:"timestamp"`
}

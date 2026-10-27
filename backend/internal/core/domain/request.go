package domain

import "time"

// The kinds of request an account can make to whoever administers the platform.
const (
	RequestEmail  = "cambio_correo" // change the e-mail of the account
	RequestExport = "exportacion"   // download and e-mail the PDF and CSV of Insights, for a while
)

// The states of a request.
const (
	RequestPending  = "pendiente"
	RequestApproved = "aprobada"
	RequestRejected = "rechazada"
)

// AccountRequest is what an account asks of its administrator and how it was answered.
type AccountRequest struct {
	ID          int64     `json:"id"`
	UserID      string    `json:"-"`
	Username    string    `json:"usuario"`
	DisplayName string    `json:"nombre,omitempty"`
	Role        string    `json:"rol"`
	Kind        string    `json:"tipo"`
	Status      string    `json:"estado"`
	NewEmail    string    `json:"correo_nuevo,omitempty"`
	Note        string    `json:"motivo,omitempty"`
	CreatedAt   time.Time `json:"creada"`
	ResolvedAt  *time.Time `json:"resuelta,omitempty"`
	ResolvedBy  string    `json:"resuelta_por,omitempty"`
	// GrantedUntil is, for an approved export request, when the permission ends.
	GrantedUntil *time.Time `json:"vigente_hasta,omitempty"`
}

// ExportState is where the permission to export stands for an account: "ninguna" (never asked), "pendiente", "vigente" (until `Until`),
// "vencida" (it ended) or "rechazada".
type ExportState struct {
	State string     `json:"estado"`
	Until *time.Time `json:"hasta,omitempty"`
	// Needed is false for the roles that never need to ask (the administrator).
	Needed bool `json:"necesita_permiso"`
}

// ExportHours are the lengths an administrator can grant, in hours; Max caps whatever else is sent.
var ExportHours = []int{1, 8, 24, 72, 168}

const MaxExportHours = 720

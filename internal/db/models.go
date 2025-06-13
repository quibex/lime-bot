package db

import "time"

type Server struct {
	ID           uint
	Name         string
	Address      string
	CAThumbprint string
	Enabled      bool
}

type Interface struct {
	ID       uint
	ServerID uint
	Name     string
	Network  string
	LastIP   string
}

// Plan - тарифы
type Plan struct {
	ID           uint      `gorm:"primaryKey"`
	Name         string    `gorm:"not null"`
	PriceInt     int       `gorm:"not null"`
	DurationDays int       `gorm:"not null"`
	Archived     bool      `gorm:"default:false"`
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP"`
}

// User - пользователи
type User struct {
	TgID      int64 `gorm:"primaryKey"`
	Username  string
	Phone     string
	RefCode   string
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP"`
}

// Admin - администраторы
type Admin struct {
	TgID     int64  `gorm:"primaryKey"`
	Role     string `gorm:"check:role IN ('super','cashier','support')"`
	Disabled bool   `gorm:"default:false"`
}

// PaymentMethod - способы оплаты (реквизиты)
type PaymentMethod struct {
	ID          uint      `gorm:"primaryKey"`
	PhoneNumber string    `gorm:"not null"`
	Bank        string    `gorm:"not null"`
	OwnerName   string    `gorm:"not null"`
	Archived    bool      `gorm:"default:false"`
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP"`
}

// Payment - платежи
type Payment struct {
	ID            uint  `gorm:"primaryKey"`
	UserID        int64 `gorm:"not null"`
	MethodID      uint  `gorm:"not null"`
	Amount        int   `gorm:"not null"`
	PlanID        uint  `gorm:"not null"`
	Qty           int   `gorm:"not null"`
	ReceiptFileID string
	Status        string `gorm:"check:status IN ('pending','approved','rejected')"`
	ApprovedBy    *int64
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP"`

	// Relations
	User            User          `gorm:"foreignKey:UserID;references:TgID"`
	Method          PaymentMethod `gorm:"foreignKey:MethodID"`
	Plan            Plan          `gorm:"foreignKey:PlanID"`
	ApprovedByAdmin *Admin        `gorm:"foreignKey:ApprovedBy;references:TgID"`
}

// Subscription - подписки (ключи)
type Subscription struct {
	ID         uint      `gorm:"primaryKey"`
	UserID     int64     `gorm:"not null"`
	PlanID     uint      `gorm:"not null"`
	PeerID     string    `gorm:"unique;not null"`
	PrivKeyEnc string    `gorm:"not null"`
	PublicKey  string    `gorm:"not null"`
	Interface  string    `gorm:"not null"`
	AllowedIP  string    `gorm:"type:text;not null"`
	Platform   string    `gorm:"not null"`
	StartDate  time.Time `gorm:"type:date;not null"`
	EndDate    time.Time `gorm:"type:date;not null"`
	Active     bool      `gorm:"default:true"`
	PaymentID  *uint

	// Relations
	User    User     `gorm:"foreignKey:UserID;references:TgID"`
	Plan    Plan     `gorm:"foreignKey:PlanID"`
	Payment *Payment `gorm:"foreignKey:PaymentID"`
}

// Referral - рефералы
type Referral struct {
	ID        uint      `gorm:"primaryKey"`
	InviterID int64     `gorm:"not null"`
	InviteeID int64     `gorm:"not null"`
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP"`

	// Relations
	Inviter User `gorm:"foreignKey:InviterID;references:TgID"`
	Invitee User `gorm:"foreignKey:InviteeID;references:TgID"`
}

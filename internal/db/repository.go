package db

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(dsn string) (*Repository, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return &Repository{db: db}, nil
}

func (r *Repository) DB() *gorm.DB {
	return r.db
}

func (r *Repository) AutoMigrate() error {
	return r.db.AutoMigrate(
		&Plan{},
		&User{},
		&Admin{},
		&PaymentMethod{},
		&Payment{},
		&Subscription{},
		&Referral{},
	)
}

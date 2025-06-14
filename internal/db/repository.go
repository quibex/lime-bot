package db

import (
	"log/slog"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(dsn string) (*Repository, error) {
	slog.Info("Инициализация репозитория", "dsn", dsn)

	dir := filepath.Dir(dsn)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

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
	// обычная миграция схемы
	if err := r.db.AutoMigrate(
		&Plan{},
		&User{},
		&Admin{},
		&PaymentMethod{},
		&Payment{},
		&Subscription{},
		&Referral{},
	); err != nil {
		return err
	}

	// ensure enum constraints are up to date
	if err := updateEnumConstraint(r.db, "admins", "role", []string{"super", "admin", "cashier", "support"}); err != nil {
		return err
	}
	if err := updateEnumConstraint(r.db, "payments", "status", []string{"pending", "approved", "rejected"}); err != nil {
		return err
	}

	return nil
}

package db

import (
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Server{},
		&Interface{},
		&Plan{},
		&User{},
		&Payment{},
		&Subscription{},
		&Admin{},
	)
}

package db

import (
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	// Сначала выполняем обычную миграцию
	err := db.AutoMigrate(
		&Server{},
		&Interface{},
		&Plan{},
		&User{},
		&Payment{},
		&Subscription{},
		&Admin{},
	)
	if err != nil {
		return err
	}

	// Обновляем constraint для ролей админов
	return updateAdminRoleConstraint(db)
}

func updateAdminRoleConstraint(db *gorm.DB) error {
	// Проверяем тип базы данных
	switch db.Dialector.Name() {
	case "sqlite":
		// SQLite не поддерживает изменение constraints, пересоздаем таблицу
		return recreateAdminTableSQLite(db)
	case "postgres":
		// PostgreSQL
		return db.Exec("ALTER TABLE admins DROP CONSTRAINT IF EXISTS chk_admins_role; ALTER TABLE admins ADD CONSTRAINT chk_admins_role CHECK (role IN ('super','admin','cashier','support'))").Error
	case "mysql":
		// MySQL
		return db.Exec("ALTER TABLE admins DROP CHECK chk_admins_role; ALTER TABLE admins ADD CONSTRAINT chk_admins_role CHECK (role IN ('super','admin','cashier','support'))").Error
	}
	return nil
}

func recreateAdminTableSQLite(db *gorm.DB) error {
	// Для SQLite создаем новую таблицу с правильным constraint
	return db.Transaction(func(tx *gorm.DB) error {
		// Создаем временную таблицу
		if err := tx.Exec(`CREATE TABLE admins_new (
			tg_id INTEGER PRIMARY KEY,
			role TEXT CHECK (role IN ('super','admin','cashier','support')),
			disabled BOOLEAN DEFAULT false
		)`).Error; err != nil {
			return err
		}

		// Копируем данные
		if err := tx.Exec("INSERT INTO admins_new (tg_id, role, disabled) SELECT tg_id, role, disabled FROM admins WHERE role IN ('super','admin','cashier','support')").Error; err != nil {
			return err
		}

		// Удаляем старую таблицу
		if err := tx.Exec("DROP TABLE admins").Error; err != nil {
			return err
		}

		// Переименовываем новую таблицу
		return tx.Exec("ALTER TABLE admins_new RENAME TO admins").Error
	})
}

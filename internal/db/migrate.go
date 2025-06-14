package db

import (
	"fmt"
	"strings"

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

	// Обновляем enum-constraint'ы
	if err := updateEnumConstraint(db, "admins", "role", []string{"super", "admin", "cashier", "support"}); err != nil {
		return err
	}
	return updateEnumConstraint(db, "payments", "status", []string{"pending", "approved", "rejected"})
}

// updateEnumConstraint гарантирует, что для столбца есть актуальный CHECK-constraint
func updateEnumConstraint(db *gorm.DB, table, column string, allowed []string) error {
	name := db.Dialector.Name()

	allowedList := "'" + strings.Join(allowed, "','") + "'"
	constraint := fmt.Sprintf("chk_%s_%s", table, column)

	switch name {
	case "postgres":
		sql := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s; ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s IN (%s))", table, constraint, table, constraint, column, allowedList)
		return db.Exec(sql).Error

	case "mysql":
		sql := fmt.Sprintf("ALTER TABLE %s DROP CHECK %s; ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s IN (%s))", table, constraint, table, constraint, column, allowedList)
		return db.Exec(sql).Error

	case "sqlite":
		return recreateSQLiteTableWithConstraint(db, table, column, allowedList, constraint)
	}
	return nil
}

// recreateSQLiteTableWithConstraint пересоздает таблицу в SQLite без потери данных
func recreateSQLiteTableWithConstraint(db *gorm.DB, table, column, allowedList, constraint string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// Получаем схему оригинальной таблицы
		var schema string
		if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Row().Scan(&schema); err != nil {
			return err
		}

		// Удаляем старый CHECK (если был) и добавляем новый
		// Простой способ: убираем строку с CHECK и добавляем свежую
		newCheck := fmt.Sprintf("CHECK (%s IN (%s))", column, allowedList)
		schema = strings.ReplaceAll(schema, constraint, "")

		// Создаем временную таблицу
		tmp := table + "_new"
		createSQL := strings.Replace(schema, table, tmp, 1)
		createSQL = strings.Replace(createSQL, ")", ", "+newCheck+")", 1)
		if err := tx.Exec(createSQL).Error; err != nil {
			return err
		}

		// Копируем данные
		if err := tx.Exec(fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", tmp, table)).Error; err != nil {
			return err
		}

		if err := tx.Exec("DROP TABLE " + table).Error; err != nil {
			return err
		}
		return tx.Exec("ALTER TABLE " + tmp + " RENAME TO " + table).Error
	})
}

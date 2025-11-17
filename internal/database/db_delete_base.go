package database

import (
	"database/sql"
	"fmt"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/freezzorg/SQLManager/internal/logging"
)

// DeleteDatabase - Удаление базы данных
func DeleteDatabase(db *sql.DB, dbName string) error {
	// Проверяем, существует ли база данных перед попыткой перевода в однопользовательский режим
	dbExists, err := checkDatabaseExists(db, dbName)
	if err != nil {
		logging.LogDebug(fmt.Sprintf("Ошибка проверки существования базы '%s': %v", dbName, err))
		// Продолжаем попытку удаления, даже если не удалось проверить существование
	} else if dbExists {
		// Если база существует, пытаемся перевести её в однопользовательский режим перед удалением
		if err := SetSingleUserMode(db, dbName); err != nil {
			// В случае ошибки при переводе в однопользовательский режим, всё равно пытаемся удалить базу
			logging.LogDebug(fmt.Sprintf("Не удалось перевести базу '%s' в однопользовательский режим перед удалением: %v. Продолжаем удаление.", dbName, err))
		}
	}
	
	// 1. Удаляем базу данных.
	// Перевод в SINGLE_USER не требуется, так как база не используется во время восстановления.
	deleteQuery := fmt.Sprintf("DROP DATABASE [%s]", dbName)
	if _, err := db.Exec(deleteQuery); err != nil {
		logging.LogWebError(fmt.Sprintf("Ошибка удаления базы данных %s: %v", dbName, err))
		return fmt.Errorf("ошибка DROP DATABASE для БД %s: %w", dbName, err)
	}
	
	logging.LogWebInfo(fmt.Sprintf("База данных '%s' успешно удалена", dbName))
	
	return nil
}

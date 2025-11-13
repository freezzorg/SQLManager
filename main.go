package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/freezzorg/SQLManager/internal/config"
	"github.com/freezzorg/SQLManager/internal/handlers"
	"github.com/freezzorg/SQLManager/internal/logging"

	// Используем стандартный драйвер для MSSQL
	_ "github.com/denisenkom/go-mssqldb"
)

// Главная функция, запускающая приложение
func main() {
    // 1. Загрузка конфигурации 
    appConfig, err := config.LoadConfig("./config.yaml")
    if err != nil {
        log.Fatalf("Ошибка загрузки конфигурации: %v", err)
    }

    // 2. Настройка логирования в файл
    logging.SetupLogger(appConfig.App.LogFile, appConfig.App.LogLevel)
    
    // 3. Установка подключения к MSSQL
    db, err := setupDBConnection(appConfig.MSSQL)
    if err != nil {
        // Мы используем LogError, который пишет и в файл
        logging.LogError(fmt.Sprintf("Ошибка подключения к SQL Server (%s): %v", appConfig.MSSQL.Server, err))
        return
    }
    logging.LogInfo("Успешное подключение к SQL Server.")
    defer db.Close() // Закрываем соединение при завершении работы приложения

    // 4. Запуск веб-сервера
    startWebServer(db, appConfig, appConfig.App.BindAddress)
}

// Устанавливает соединение с SQL Server
func setupDBConnection(cfg struct {
	Server   string    `yaml:"server"`
	Port     int       `yaml:"port"`
	User     string    `yaml:"user"`
	Password string    `yaml:"password"`
	RestorePath string `yaml:"restore_path"` 
}) (*sql.DB, error) {
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", cfg.Server, cfg.User, cfg.Password, cfg.Port)

    db, err := sql.Open("sqlserver", connString)
    if err != nil {
        return nil, err
    }
    // Проверка соединения
    if err := db.Ping(); err != nil {
        return nil, err
    }
    return db, nil
}

// Запускает веб-сервер
func startWebServer(db *sql.DB, appConfig *config.Config, addr string) {
    // Настройка маршрутов
    // Обслуживание статических файлов из директории "static"
    http.Handle("/", http.FileServer(http.Dir("./static")))
    
    // Создаем экземпляр AppHandlers
    appHandlers := &handlers.AppHandlers{DB: db, AppConfig: appConfig}

    // API маршруты:
    http.HandleFunc("/api/databases", appHandlers.AuthMiddleware(appHandlers.HandleGetDatabases))
    http.HandleFunc("/api/delete", appHandlers.AuthMiddleware(appHandlers.HandleDeleteDatabase)) 
    http.HandleFunc("/api/backups", appHandlers.AuthMiddleware(appHandlers.HandleGetBackups))
    http.HandleFunc("/api/restore", appHandlers.AuthMiddleware(appHandlers.HandleStartRestore)) 
    http.HandleFunc("/api/log", appHandlers.AuthMiddleware(appHandlers.HandleGetLog))
    http.HandleFunc("/api/cancel-restore", appHandlers.AuthMiddleware(appHandlers.HandleCancelRestoreProcess)) 
    http.HandleFunc("/api/restore-progress", appHandlers.AuthMiddleware(appHandlers.HandleGetRestoreProgress))
    http.HandleFunc("/api/backup", appHandlers.AuthMiddleware(appHandlers.HandleStartBackup))
    http.HandleFunc("/api/backup-progress", appHandlers.AuthMiddleware(appHandlers.HandleGetBackupProgress))
    http.HandleFunc("/api/backup-metadata", appHandlers.AuthMiddleware(appHandlers.HandleGetBackupMetadata))

    logging.LogInfo(fmt.Sprintf("Веб-сервер запущен на %s", addr))
    // Запускаем веб-сервер
    if err := http.ListenAndServe(addr, nil); err != nil {
        logging.LogError(fmt.Sprintf("Ошибка запуска веб-сервера: %v", err))
    }
}

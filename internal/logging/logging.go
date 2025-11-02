package logging

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freezzorg/SQLManager/internal/config"
)

var fileLogger *log.Logger
var currentLogLevel int // 0: ERROR, 1: INFO, 2: DEBUG
var logMutex sync.Mutex // Мьютекс для безопасной записи в лог-файл
var briefLog []config.LogEntry // Краткий лог для веб-интерфейса

func SetupLogger(logFile string, level string) {
    // Определение уровня логирования
    switch strings.ToUpper(level) {
    case "DEBUG":
        currentLogLevel = 2
    case "INFO":
        currentLogLevel = 1
    default: // По умолчанию ERROR
        currentLogLevel = 0
    }

    // Создание каталога, если он не существует
    dir := filepath.Dir(logFile)
    if err := os.MkdirAll(dir, 0755); err != nil {
        log.Fatalf("Не удалось создать каталог для лог-файла: %v", err)
    }

    // Настройка файла логов
    file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
    if err != nil {
        log.Fatalf("Не удалось открыть лог-файл: %v", err)
    }
    fileLogger = log.New(file, "", log.Ldate|log.Ltime)
    LogInfo("Логирование настроено.")
}

// Запись в краткий лог (для веб-интерфейса)
func recordBriefLog(message string) {
    logMutex.Lock()
    defer logMutex.Unlock()
    
    entry := config.LogEntry{
        Timestamp: time.Now(),
        Message:   message,
    }
    briefLog = append(briefLog, entry)

    // Ограничение размера краткого лога (например, 50 записей)
    if len(briefLog) > 50 {
        briefLog = briefLog[len(briefLog)-50:]
    }
}

// Функция логирования DEBUG
func LogDebug(message string) {
    if currentLogLevel >= 2 {
        fileLogger.Printf("[DEBUG] %s", message)
    }
}

// Функция логирования INFO (только в файл)
func LogInfo(message string) {
    if currentLogLevel >= 1 {
        fileLogger.Printf("[INFO] %s", message)
    }
}

// Функция логирования ERROR (только в файл)
func LogError(message string) {
    // ERROR логируется всегда
    fileLogger.Printf("[ERROR] %s", message)
}

// RecordWebLog - Запись сообщения в краткий лог для веб-интерфейса
func RecordWebLog(message string) {
    recordBriefLog(message)
}

// Функция логирования INFO для веб-интерфейса (и в файл)
func LogWebInfo(message string) {
    if currentLogLevel >= 1 {
        fileLogger.Printf("[INFO] %s", message)
        RecordWebLog(message) // Запись в краткий лог для веб-интерфейса
    }
}

// Функция логирования ERROR для веб-интерфейса (и в файл)
func LogWebError(message string) {
    fileLogger.Printf("[ERROR] %s", message)
    RecordWebLog("ОШИБКА: " + message) // Запись в краткий лог для веб-интерфейса
}

// GetBriefLog - Получение краткого лога
func GetBriefLog() []config.LogEntry {
    logMutex.Lock()
    defer logMutex.Unlock()
    
    // Создаем копию лога, чтобы избежать гонок
    logCopy := make([]config.LogEntry, len(briefLog))
    copy(logCopy, briefLog)
    
    return logCopy
}

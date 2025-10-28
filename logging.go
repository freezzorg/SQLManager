package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var fileLogger *log.Logger
var currentLogLevel int // 0: ERROR, 1: INFO, 2: DEBUG

func setupLogger(logFile string, level string) {
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

    // Настройка файла логов [cite: 15]
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
    
    entry := LogEntry{
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

// Функция логирования INFO
func LogInfo(message string) {
    if currentLogLevel >= 1 {
        fileLogger.Printf("[INFO] %s", message)
        recordBriefLog(message) // Запись в краткий лог [cite: 16]
    }
}

// Функция логирования ERROR
func LogError(message string) {
    fileLogger.Printf("[ERROR] %s", message)
    recordBriefLog("ОШИБКА: " + message) // Запись в краткий лог [cite: 16]
}

// Функция логирования WARNING (можно использовать для неосновных ошибок)
func LogWarning(message string) {
    if currentLogLevel >= 1 {
        fileLogger.Printf("[WARNING] %s", message)
    }
}

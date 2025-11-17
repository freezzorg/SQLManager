package logging

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freezzorg/SQLManager/internal/config"
)

var fileLogger *log.Logger
var userMessageLogger *log.Logger
var currentLogLevel int // 0: ERROR, 1: INFO, 2: DEBUG
var logMutex sync.Mutex // Мьютекс для безопасной записи в лог-файл
var briefLog []config.LogEntry // Краткий лог для веб-интерфейса (последние 50 сообщений)
var fullHistoryLog []config.LogEntry // Полная история сообщений для веб-интерфейса (до 500 сообщений)

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
    
    // Настройка файла логов для сообщений пользователю
    userMessageLogFile := filepath.Join(dir, "user_messages.log")
    userMessageFile, err := os.OpenFile(userMessageLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
    if err != nil {
        log.Fatalf("Не удалось открыть файл логов сообщений пользователю: %v", err)
    }
    userMessageLogger = log.New(userMessageFile, "", log.Ldate|log.Ltime)
    
    // Загружаем существующие сообщения пользователю в briefLog при запуске
    loadUserMessagesToBriefLog(userMessageLogFile)
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
    fullHistoryLog = append(fullHistoryLog, entry)

    // Ограничение размера краткого лога (например, 50 записей)
    if len(briefLog) > 50 {
        briefLog = briefLog[len(briefLog)-50:]
    }
    
    // Ограничение размера полного лога (500 записей)
    if len(fullHistoryLog) > 500 {
        fullHistoryLog = fullHistoryLog[len(fullHistoryLog)-500:]
    }
    
    // Записываем сообщение также в файл логов пользовательских сообщений
    userMessageLogger.Printf("%s %s", entry.Timestamp.Format("2006-01-02 15:04:05"), entry.Message)
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

// GetBriefLog - Получение краткого лога (до 50 последних записей)
func GetBriefLog() []config.LogEntry {
    logMutex.Lock()
    defer logMutex.Unlock()
    
    // Создаем копию лога, чтобы избежать гонок
    logCopy := make([]config.LogEntry, len(briefLog))
    copy(logCopy, briefLog)
    
    return logCopy
}

// GetFullHistoryLog - Получение полной истории лога (все доступные записи)
func GetFullHistoryLog() []config.LogEntry {
    logMutex.Lock()
    defer logMutex.Unlock()
    
    // Создаем копию всего лога
    logCopy := make([]config.LogEntry, len(fullHistoryLog))
    copy(logCopy, fullHistoryLog)
    
    return logCopy
}

// loadUserMessagesToBriefLog - Загрузка сообщений пользователю из файла в briefLog при запуске
func loadUserMessagesToBriefLog(logFilePath string) {
    file, err := os.Open(logFilePath)
    if err != nil {
        // Если файл не существует, просто возвращаемся
        if os.IsNotExist(err) {
            return
        }
        log.Printf("Ошибка открытия файла логов сообщений пользователю: %v", err)
        return
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    var messages []config.LogEntry

    for scanner.Scan() {
        line := scanner.Text()
        // Парсим строку лога: дата время сообщение
        parts := strings.SplitN(line, " ", 3)
        if len(parts) >= 3 {
            timestampStr := parts[0] + " " + parts[1]
            message := parts[2]

            timestamp, err := time.Parse("2006-01-02 15:04:05", timestampStr)
            if err != nil {
                continue
            }

            messages = append(messages, config.LogEntry{
                Timestamp: timestamp,
                Message:   message,
            })
        }
    }

    // Ограничиваем количество загружаемых сообщений до 500
    if len(messages) > 500 {
        messages = messages[len(messages)-500:]
    }

    logMutex.Lock()
    defer logMutex.Unlock()
    
    // Добавляем загруженные сообщения в оба лога
    briefLog = append(briefLog, messages...)
    fullHistoryLog = append(fullHistoryLog, messages...)
    
    // Ограничиваем размер briefLog до 50 (для веб-интерфейса)
    if len(briefLog) > 50 {
        briefLog = briefLog[len(briefLog)-50:]
    }
    
    // Ограничиваем размер fullHistoryLog до 500
    if len(fullHistoryLog) > 500 {
        fullHistoryLog = fullHistoryLog[len(fullHistoryLog)-500:]
    }
}

package main

import (
	"time"
)

// Конфигурационная структура, соответствующая config.yaml
type Config struct {
    MSSQL struct {
        Server      string `yaml:"server"`
        Port        int    `yaml:"port"`
        User        string `yaml:"user"`
        Password    string `yaml:"password"`
        RestorePath string `yaml:"restore_path"` // /var/opt/mssql/data
    } `yaml:"mssql"`
    SMBShare struct {
        RemotePath      string `yaml:"remote_path"`
        LocalMountPoint string `yaml:"local_mount_point"` // /mnt/sql_backups
        Domain          string `yaml:"domain"`
        User            string `yaml:"user"`
        Password        string `yaml:"password"`
    } `yaml:"smb_share"`
    App struct {
        BindAddress string   `yaml:"bind_address"`
        LogFile     string   `yaml:"log_file"`
        LogLevel    string   `yaml:"log_level"`
        BackupBlacklist []string `yaml:"backup_blacklist"` // Черный список бэкапов
    } `yaml:"app"`
    Whitelist []string `yaml:"whitelist"` // Белый список IP-адресов
}

// Структура для отображения базы данных в веб-интерфейсе
type Database struct {
    Name       string `json:"name"`
    State      string `json:"state"` // Состояние базы (online, offline, restoring, error)
}

// Структура для отображения бэкапа
type BackupFile struct {
    FileName  string       `json:"fileName"`
    // Type      string       `json:"type"` // .bak, .diff, .trn [cite: 4] - Удаляем, так как для списка директорий не нужен
    // BackupDate sql.NullTime `json:"backupDate"` // Дата и время из метаданных [cite: 14], может быть NULL - Удаляем
}

// Структура для краткого лога действий [cite: 16]
type LogEntry struct {
    Timestamp time.Time `json:"timestamp"`
    Message   string    `json:"message"`
}

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
        BindAddress string `yaml:"bind_address"`
        LogFile     string `yaml:"log_file"`
        LogLevel    string `yaml:"log_level"`
    } `yaml:"app"`
    Whitelist []string `yaml:"whitelist"`
}

// Структура для отображения базы данных в веб-интерфейсе
type Database struct {
    Name       string `json:"name"`
    State      string `json:"state"` // Состояние базы (ONLINE, RESTORING и т.д.)
}

// Структура для отображения бэкапа
type BackupFile struct {
    FileName  string    `json:"fileName"`
    Type      string    `json:"type"` // .bak, .diff, .trn [cite: 4]
    BackupDate time.Time `json:"backupDate"` // Дата и время из метаданных [cite: 14]
}

// Структура для краткого лога действий [cite: 16]
type LogEntry struct {
    Timestamp time.Time `json:"timestamp"`
    Message   string    `json:"message"`
}
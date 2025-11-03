# SQLManager
> Проект создан при помощи ~~смекалки и деатомайзера 7-й серии~~ чат-бота Gemini.
------------

## Обзор проекта

SQLManager - это веб-приложение для управления базами данных SQL Server, позволяющее выполнять операции восстановления и создания бэкапов через простой веб-интерфейс. Приложение разработано с учетом безопасности и доступности, используя пул соединений с базой данных, валидацию входных данных и подробное логирование.

## Установка и настройка:
- Скачиваем DEB пакет и делаем его исполняемым
```bash
sqlmanager-1.0.0-amd64.deb
chmod +x sqlmanager-1.0.0-amd64.deb
```
- Устанавливаем его
```bash
sudo dpkg -i sqlmanager-1.0.0-amd64.deb
```
- В файл /etc/smbcredentials/.veeamsrv_creds добавить данные в виде:
```bash
username=имя
pssword=пароль
domain=ДОМЕН
```
- Назначить права доступа:
```bash
sudo cmod 640 /etc/smbcredentials/.veeamsrv_creds
```
- Настраиваем конфигурационный файл /opt/SQLManager/config.yaml
- Перезапускаем службу
```bash
sudo systemctl restart sqlmanager
```

## Ручная установка и настройка
- Клонировать проект и расположить его, там где он будет работать:
```bash
git clone https://github.com/freezzorg/SQLManager.git
mv SQLManager /opt
```
- Создаём необходимые каталоги
```bash
mkdir -p /var/log/sqlmanager
mkdir -p /mnt/sql_backups
mkdir -p /etc/smbcredentials
```

- Устанавливаем права на директории и файлы
```bash
chown -R mssql:mssql /opt/SQLManager
chown -R mssql:mssql /var/log/sqlmanager
```

- Устанавливаем права: директории с rwx, файлы с rw
```bash
find /opt/SQLManager -type d -exec chmod 755 {} \;
find /opt/SQLManager -type f -exec chmod 644 {} \;
```

- Делаем исполняемым основной бинарник
```bash
chmod +x /opt/SQLManager/sqlmanager
```

- Устанавливаем специальные права для конфигурационного файла
```bash
chmod 600 /opt/SQLManager/config.yaml
```

- Устанавливаем права на лог-файл
```bash
chmod 640 /var/log/sqlmanager/sqlmanager.log
chmod 750 /var/log/sqlmanager
```

- Создаем файл в /etc/sudoers.d для выполнения команд монтирования
```bash
echo "mssql ALL=(ALL) NOPASSWD: /bin/systemctl start mnt-sql_backups.mount, /bin/systemctl status mnt-sql_backups.mount" > /etc/sudoers.d/sqlmanager
chmod 440 /etc/sudoers.d/sqlmanager
```

- Настроить монтирования windows-шары при загрузке сервера через systemd:
```bash
sudo nano /etc/systemd/system/mnt-sql_backups.mount
```
```bash
[Unit]
Description=SMB/CIFS Mount for SQL Backups
Requires=network-online.target
After=network-online.target

[Mount]
What=//veeamsrv.kcep.local/backup$/mssql
Where=/mnt/sql_backups
Type=cifs
Options=vers=3.0,credentials=/etc/smbcredentials/.veeamsrv_creds,uid=mssql,gid=mssql,file_mode=0660,dir_mode=0770,_netdev

[Install]
WantedBy=multi-user.target
```
- В файл /etc/smbcredentials/.veeamsrv_creds добавить данные в виде:
```bash
username=имя
pssword=пароль
domain=ДОМЕН
```
- Назначить права доступа:
```bash
sudo cmod 640 /etc/smbcredentials/.veeamsrv_creds
```

- Ручной запуск:
```bash
sudo /opt/SQLManager/sqlmanager
```

- Запуск через systemd:
```bash
sudo nano /etc/systemd/system/sqlmanager.service
```
```bash
[Unit]
Description=SQLManager Web Application
After=network.target

[Service]
User=mssql
Group=mssql
WorkingDirectory=/opt/SQLManager
ExecStart=/opt/SQLManager/sqlmanager
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
- Перезагружаем systemd, включаем и запускаем сервисы
```bash
sudo systemctl daemon-reload
sudo systemctl enable sqlmanager.service
sudo systemctl start sqlmanager.service
sudo systemctl enable mnt-sql_backups.mount
sudo systemctl start mnt-sql_backups.mount
```
- Настраиваем ротацию логов
```bash
sudo nano /etc/logrotate.d/sqlmanager
```
```bash
/var/log/sqlmanager/sqlmanager.log {
    monthly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    create 640 mssql mssql
    postrotate
    endscript
}
```

## Конфигурация (`config.yaml`)

Приложение использует файл `config.yaml` для настройки подключения к SQL Server, параметров SMB-шары и других настроек. Пример файла `config.yaml`:
```yaml
# Настройки подключения к SQL Server
mssql:
  server: "USQL1" # Имя тестового сервера 
  port: 1433
  user: "sa" # Имя пользователя SQL
  password: "Jc/x2no@" # Пароль SQL
  # Путь для перемещения файлов данных/логов при восстановлении
  restore_path: "/var/opt/mssql/data" # Указанный каталог

# Настройки доступа к Windows-шаре (для бэкапов)
smb_share:
  remote_path: "//veeamsrv.kcep.local/backup$/mssql" # Удаленный путь к шаре
  local_mount_point: "/mnt/sql_backups" # Локальная точка монтирования

# Настройки приложения и безопасности
app:
  bind_address: "0.0.0.0:8088"
  log_file: "/var/log/sqlmanager/sqlmanager.log" # Путь к файлу логов
  log_level: "DEBUG" # Уровень логирования (INFO, ERROR, DEBUG)
  backup_blacklist: # Черный список бэкапов
    - "-=NoUsedBaseBackups=-"
    - "-=scripts=-"
    - "-=SQL1=-"
    - "autojournal"
    - "Forbest_test"
    - "FullBackupBases"
    - "Kazcentrelektroprvod2010"
    - "master-mssql"
    - "master-nsql"
    - "master-usql"
    - "master-wms"
    - "msdb-mssql"
    - "msdb-nsql"
    - "msdb-usql"
    - "msdb-wms"
    - "test"
    - "test_upp"
    - "test_upp_forbitrix24"
    - "wms"

# Белый список IP-адресов/хостов для доступа к веб-интерфейсу 
whitelist:
  - "127.0.0.1"
  - "10.10.100.40"
  - "10.10.100.49"
  - "10.10.102.122"
  - "10.10.102.184"
  - "10.10.100.56"
```
## Устранение ошибок

Если при запуске приложения возникает ошибка типа:
```bash
./sqlmanager: /lib/x86_64-linux-gnu/libc.so.6: version `GLIBC_2.34' not found (required by ./sqlmanager)
./sqlmanager: /lib/x86_64-linux-gnu/libc.so.6: version `GLIBC_2.32' not found (required by ./sqlmanager)
```
то это говорит о том, что мы собираем исполняемый файл Go на более новой версии операционной системы
(или в контейнере с более новой версией GLIBC), а затем пытаетесь запустить его на целевом сервере с более старой версией GLIBC (GNU C Library).

Необходимо,
- либо собрать проект в версии операционной системе, используемой на сервере,
- либо собрать проект, используя статическую компиляцию Go
```bash
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags=-static -X main.version=1.0.0" -a -tags netgo -o sqlmanager
```
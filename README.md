# SQLManager
> Проект создан при помощи ~~смекалки и деатомайзера 7-й серии~~ чат-бота Gemini.
------------

## Обзор проекта

SQLManager - это веб-приложение для управления базами данных SQL Server, позволяющее выполнять операции восстановления и создания бэкапов через простой веб-интерфейс. Приложение разработано с учетом безопасности и доступности, используя пул соединений с базой данных, валидацию входных данных и подробное логирование.

## Установка и настройка:

- Клонировать проект и расположить его, там где он будет работать:
```bash
git clone https://github.com/freezzorg/SQLManager.git
mv SQLManager /opt
sudo chown -R sql:"пользователи домена" /opt/SQLManager
sudo chmod -R 750 /opt/SQLManager
sudo chown -R sql:"пользователи домена" /mnt/sql_backups
sudo chmod -R 750 /mnt/sql_backups
```

- Настроить монтирования windows-шары при загрузке сервера через systemd:
```bash
sudo nano /etc/systemd/system/mnt-sql_backups.mount
```
```bash
[Unit]
Description=CIFS Share Mount for SQL Backups
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
- Добавить пользователя sql в группу mssql, что бы приложение могло читать данные их шары:
```bash
sudo usermod -aG mssql sql
```

- Настроить sudo для sql, чтобы SQLManager мог выполнять mount и umount без пароля:
```bash
sudo visudo

sql ALL=(ALL) NOPASSWD: /bin/mount -t cifs *
````

- Ручной запуск:
```bash
pwsh /opt/SQLManager/sqlmanager
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
User=sql
Group="пользователи домена"
WorkingDirectory=/opt/SQLManager
ExecStart=/opt/SQLManager/sqlmanager
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
```bash
sudo systemctl daemon-reload
sudo systemctl enable sqlmanager.service
sudo systemctl start sqlmanager.service
```

## Конфигурация (`config.yaml`)

Приложение использует файл `config.yaml` для настройки подключения к SQL Server, параметров SMB-шары и других настроек. Пример файла `config.yaml`:

```yaml
app:
  bind_address: ":8080"
  log_file: "/var/log/sqlmanager/app.log"
  log_level: "INFO" # DEBUG, INFO, ERROR
  backup_blacklist:
    - "tempdb"
    - "model"
    - "msdb"
    - "master"
mssql:
  server: "your_sql_server_ip"
  port: 1433
  user: "your_sql_user"
  password: "your_sql_password"
  restore_path: "/var/opt/mssql/data" # Путь, куда будут восстанавливаться файлы БД на SQL Server
smb_share:
  local_mount_point: "/mnt/sql_backups" # Локальная точка монтирования SMB-шары
whitelist:
  - "127.0.0.1" # Разрешенные IP-адреса для доступа к веб-интерфейсу
```

## Валидация входных данных

Приложение выполняет валидацию имен баз данных и путей к бэкапам для предотвращения некорректных или вредоносных запросов. Имена баз данных должны состоять из букв, цифр, подчеркиваний и дефисов, не должны начинаться с цифры или дефиса, и иметь длину от 1 до 128 символов. Имена директорий бэкапов также ограничены безопасными символами и длиной от 1 до 255 символов.

## Логирование

Приложение использует систему логирования, которая записывает события в файл (`app.log`) и в краткий лог для отображения в веб-интерфейсе. Уровень логирования можно настроить в `config.yaml` (`DEBUG`, `INFO`, `ERROR`).

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

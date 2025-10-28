# SQLManager
> Проект создан при помощи ~~смекалки и деатомайзера 7-й серии~~ чат-бота Gemini.
------------
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

- Настроить /etc/fstab для монтирования windows-шары при загрузке сервера:
```bash
sudo mount -t cifs \
  -o username=sql,password=sql,domain=kcep,vers=3.0,uid=mssql,gid=mssql,file_mode=0660,dir_mode=0770 \
  //veeamsrv.kcep.local/backup$/mssql /mnt/sql_backups
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
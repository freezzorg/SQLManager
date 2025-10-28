# SQLManager
> Проект создан при помощи ~~смекалки и деатомайзера 7-й серии~~ чат-бота Gemini.
------------
## Установка и настройка:

- Клонировать проект и расположить его, там где он будет работать:
```bash
git clone https://github.com/freezzorg/SQLManager.git
mv web-sql /opt
sudo chown -R sql:"пользователи домена" /opt/web-sql
sudo chmod -R 750 /opt/web-sql
sudo chown -R sql:"пользователи домена" /mnt/mssql
sudo chmod -R 750 /mnt/mssql
```

- Настроить sudo для sql, чтобы websql мог выполнять mount и umount без пароля:
```bash
sudo visudo

sql ALL=(root) NOPASSWD: /bin/mount, /bin/umount
````

- Ручной запуск:
```bash
pwsh /opt/web-sql/web-sql.ps1
```

- Запуск через systemd:
```bash
sudo nano /etc/systemd/system/web-sql.service
```
```bash
[Unit]
Description=Web SQL Service
After=network.target

[Service]
ExecStart=/usr/bin/pwsh -File /opt/web-sql/web-sql.ps1
Restart=always
User=sql
#Group="пользователи домена"
WorkingDirectory=/opt/web-sql/
Environment="PATH=/usr/local/bin:/usr/bin:/bin"

[Install]
WantedBy=multi-user.target
```
```bash
sudo systemctl daemon-reload
sudo systemctl enable web-sql.service
sudo systemctl start web-sql.service
```

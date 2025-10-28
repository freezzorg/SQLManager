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

- Настроить sudo для sql, чтобы SQLManager мог выполнять mount и umount без пароля:
```bash
sudo visudo

sql ALL=(ALL) NOPASSWD: /usr/bin/mount -t cifs *
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
Group=sql
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

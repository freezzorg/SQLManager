#!/bin/bash

# Скрипт для сборки deb-пакета SQLManager

set -e  # Прерывать выполнение при ошибках

echo "Начинаем сборку deb-пакета для SQLManager..."

# Проверяем, установлен ли Go
if ! command -v go &> /dev/null; then
    echo "Go не установлен. Установите Go перед продолжением."
    exit 1
fi

# Проверяем, установлен ли dpkg-deb
if ! command -v dpkg-deb &> /dev/null; then
    echo "dpkg-deb не установлен. Установите dpkg-dev перед продолжением."
    sudo apt-get update
    sudo apt-get install -y dpkg-dev
fi

# Создаем директории для пакета
mkdir -p debian/DEBIAN
mkdir -p debian/opt/SQLManager
mkdir -p debian/usr/bin
mkdir -p debian/etc/systemd/system
mkdir -p debian/etc/sqlmanager
mkdir -p debian/var/log/sqlmanager
mkdir -p debian/etc/smbcredentials

# Копируем файлы приложения
cp -r static debian/opt/SQLManager/
cp main.go go.mod go.sum debian/opt/SQLManager/ 2>/dev/null || true
cp config.yaml debian/opt/SQLManager/ 2>/dev/null || true
cp mnt-sql_backups.mount debian/opt/SQLManager/

# Собираем приложение (создаем статическую сборку для совместимости)
echo "Собираем приложение..."
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags=-static" -a -tags netgo -o debian/opt/SQLManager/sqlmanager .

# Копируем исполняемый файл в /usr/bin (создаем символическую ссылку или копируем)
# ln -s /opt/SQLManager/sqlmanager debian/usr/bin/sqlmanager

# Копируем конфигурационный файл
cp debian/etc/sqlmanager/config.yaml debian/opt/SQLManager/config.yaml

# Копируем файл монтирования SMB-шары
cp mnt-sql_backups.mount debian/etc/systemd/system/

# Создаем скрипт запуска
cat > debian/opt/SQLManager/start.sh << 'EOF'
#!/bin/bash
cd /opt/SQLManager
exec ./sqlmanager
EOF

chmod +x debian/opt/SQLManager/start.sh

# Устанавливаем права доступа
chmod +x debian/opt/SQLManager/sqlmanager

# Создаем postinst скрипт (выполняется после установки)
cat > debian/DEBIAN/postinst << 'EOF'
#!/bin/bash
set -e

# Создаем пользователя sql, если не существует
if ! id "sql" &>/dev/null; then
    useradd -r -s /bin/false sql
fi

# Создаем пользователя mssql, если не существует (для монтирования)
if ! id "mssql" &>/dev/null; then
    useradd -r -s /bin/false mssql
fi

# Создаем директории, если не существуют
mkdir -p /var/log/sqlmanager
mkdir -p /mnt/sql_backups
mkdir -p /etc/smbcredentials

# Устанавливаем права на директории и файлы
chown -R sql:sql /opt/SQLManager
chown -R sql:sql /var/log/sqlmanager
chmod -R 750 /opt/SQLManager
chmod -R 750 /var/log/sqlmanager

# Копируем конфигурационный файл, если не существует
if [ ! -f /etc/sqlmanager/config.yaml ]; then
    mkdir -p /etc/sqlmanager
    cp /opt/SQLManager/config.yaml /etc/sqlmanager/config.yaml
    chown sql:sql /etc/sqlmanager/config.yaml
    chmod 600 /etc/sqlmanager/config.yaml
fi

# Копируем файл монтирования, если не существует
if [ ! -f /etc/systemd/system/mnt-sql_backups.mount ]; then
    cp /opt/SQLManager/mnt-sql_backups.mount /etc/systemd/system/
    systemctl enable mnt-sql_backups.mount || true
fi

# Перезагружаем systemd
systemctl daemon-reload || true

# Включаем и запускаем сервисы
systemctl enable sqlmanager.service || true
systemctl start sqlmanager.service || true

echo "SQLManager установлен. Пожалуйста, настройте /etc/sqlmanager/config.yaml и /etc/smbcredentials/.veeamsrv_creds перед использованием."
EOF

# Создаем prerm скрипт (выполняется перед удалением)
cat > debian/DEBIAN/prerm << 'EOF'
#!/bin/bash
set -e

# Останавливаем и отключаем сервис
systemctl stop sqlmanager.service || true
systemctl disable sqlmanager.service || true

exit 0
EOF

# Создаем postrm скрипт (выполняется после удаления)
cat > debian/DEBIAN/postrm << 'EOF'
#!/bin/bash
set -e

# Удаляем пользователя sql, если он был создан пакетом
# (в реальной реализации нужно быть осторожным с удалением пользователей)

# Перезагружаем systemd
systemctl daemon-reload || true

exit 0
EOF

chmod +x debian/DEBIAN/postinst debian/DEBIAN/prerm debian/DEBIAN/postrm

# Устанавливаем права на исполняемые файлы
chmod +x debian/usr/bin/sqlmanager

# Обновляем контрольный файл с размером пакета
SIZE=$(du -sb debian/opt/SQLManager | cut -f1)
sed -i "s/^Installed-Size:.*$//g" debian/DEBIAN/control
echo "Installed-Size: $((SIZE/1024 + 1))" >> debian/DEBIAN/control

# Создаем пакет
echo "Создаем deb-пакет..."
dpkg-deb --build debian sqlmanager-1.0.0-amd64.deb

echo "Сборка завершена. Пакет: sqlmanager-1.0.0-amd64.deb"

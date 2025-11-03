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
mkdir -p debian/var/log/sqlmanager
mkdir -p debian/etc/smbcredentials
mkdir -p debian/etc/logrotate.d

# Копируем файлы приложения
cp -r static debian/opt/SQLManager/
# Копируем пример конфигурационного файла
cp config.yaml debian/opt/SQLManager/config.yaml.example 2>/dev/null || true

# Копируем файл ротации лога
cp sources/sqlmanager debian/etc/logrotate.d/

# Копируем файлы сервисов systemd
cp sources/mnt-sql_backups.mount debian/etc/systemd/system/
cp sources/sqlmanager.service debian/etc/systemd/system/

# Собираем приложение (создаем статическую сборку для совместимости)
echo "Собираем приложение..."
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags=-static" -a -tags netgo -o debian/opt/SQLManager/sqlmanager .

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

# Создаем пользователя mssql, если не существует (для монтирования)
if ! id "mssql" &>/dev/null; then
    useradd -r -s /bin/false mssql
fi

# Создаем директории, если не существуют
mkdir -p /var/log/sqlmanager
mkdir -p /mnt/sql_backups
mkdir -p /etc/smbcredentials

# Копируем пример конфигурационного файла, если config.yaml не существует
if [ ! -f /opt/SQLManager/config.yaml ]; then
    cp /opt/SQLManager/config.yaml.example /opt/SQLManager/config.yaml
fi

# Устанавливаем права на директории и файлы
chown -R mssql:mssql /opt/SQLManager
chown -R mssql:mssql /var/log/sqlmanager

# Устанавливаем права: директории с rwx, файлы с rw
find /opt/SQLManager -type d -exec chmod 755 {} \;
find /opt/SQLManager -type f -exec chmod 644 {} \;

# Делаем исполняемым основной бинарник
chmod +x /opt/SQLManager/sqlmanager

# Устанавливаем специальные права для конфигурационного файла
if [ -f /opt/SQLManager/config.yaml ]; then
    chmod 600 /opt/SQLManager/config.yaml
fi
# Устанавливаем права для примера конфигурационного файла
if [ -f /opt/SQLManager/config.yaml.example ]; then
    chmod 644 /opt/SQLManager/config.yaml.example
fi

# Устанавливаем права на лог-файл, если он существует
if [ -f /var/log/sqlmanager/sqlmanager.log ]; then
    chmod 640 /var/log/sqlmanager/sqlmanager.log
fi
chmod 750 /var/log/sqlmanager

# Создаем файл в /etc/sudoers.d для выполнения команд монтирования
echo "mssql ALL=(ALL) NOPASSWD: /bin/systemctl start mnt-sql_backups.mount, /bin/systemctl status mnt-sql_backups.mount" > /etc/sudoers.d/sqlmanager
chmod 440 /etc/sudoers.d/sqlmanager

# Перезагружаем systemd
systemctl daemon-reload || true

# Включаем и запускаем сервисы
systemctl enable mnt-sql_backups.mount || true
systemctl start mnt-sql_backups.mount || true
systemctl enable sqlmanager.service || true
systemctl start sqlmanager.service || true

echo
echo "SQLManager установлен. Пожалуйста, настройте /opt/SQLManager/config.yaml и /etc/smbcredentials/.veeamsrv_creds перед использованием."
echo "После настройки запустите приложение командой: sudo systemctl start sqlmanager.service или перезагрузите сервер."
EOF

# Создаем prerm скрипт (выполняется перед удалением)
cat > debian/DEBIAN/prerm << 'EOF'
#!/bin/bash
set -e

# Останавливаем и отключаем сервис
systemctl stop sqlmanager.service || true
systemctl disable sqlmanager.service || true

# Удаляем файл sudoers, если он существует
if [ -f /etc/sudoers.d/sqlmanager ]; then
    rm -f /etc/sudoers.d/sqlmanager
fi

exit 0
EOF

# Создаем postrm скрипт (выполняется после удаления)
cat > debian/DEBIAN/postrm << 'EOF'
#!/bin/bash
set -e

# Удаляем файл sudoers, если он существует
if [ -f /etc/sudoers.d/sqlmanager ]; then
    rm -f /etc/sudoers.d/sqlmanager
fi

# Перезагружаем systemd
systemctl daemon-reload || true

exit 0
EOF

chmod +x debian/DEBIAN/postinst debian/DEBIAN/prerm debian/DEBIAN/postrm

# Устанавливаем права на исполняемые файлы
chmod +x debian/usr/bin/sqlmanager

# Обновляем контрольный файл с размером пакета
SIZE=$(du -sb debian/opt/SQLManager | cut -f1)
if grep -q "^Installed-Size:" debian/DEBIAN/control; then
    sed -i "s/^Installed-Size:.*$/Installed-Size: $((SIZE/1024 + 1))/" debian/DEBIAN/control
else
    # Если строки Installed-Size нет, добавляем её перед Description
    sed -i '/^Description:/i\Installed-Size: '"$((SIZE/1024 + 1))"'' debian/DEBIAN/control
fi

# Создаем пакет
echo "Создаем deb-пакет..."
dpkg-deb --build debian build/sqlmanager-1.0.1-amd64.deb

echo "Сборка завершена. Пакет: sqlmanager-1.0.1-amd64.deb"

#!/bin/bash

# Скрипт для сборки deb-пакета SQLManager

set -e  # Прерывать выполнение при ошибках

echo "Начинаем сборку deb-пакета для SQLManager..."

# Функция для увеличения patch версии
increment_patch_version() {
    local version_file="debian/DEBIAN/control"
    local current_version=$(grep "^Version:" "$version_file" | cut -d' ' -f2)
    echo "Текущая версия: $current_version"
    
    # Разбиваем версию на части
    IFS='.' read -ra version_parts <<< "$current_version"
    local major="${version_parts[0]}"
    local minor="${version_parts[1]}"
    local patch="${version_parts[2]}"
    
    # Увеличиваем patch версию
    new_patch=$((patch + 1))
    new_version="$major.$minor.$new_patch"
    
    # Обновляем версию в файле control
    sed -i "s/^Version:.*/Version: $new_version/" "$version_file"
    
    echo "Новая версия: $new_version"
    echo "$new_version" > .build_version  # Сохраняем версию в файл для использования ниже
    
    # Обновляем версию в файле примера конфига
    if [ -f "config.yaml" ]; then
        sed -i "s/version: $current_version/version: $new_version/" "config.yaml" 2>/dev/null || true
    fi
}

# Увеличиваем patch версию перед сборкой
increment_patch_version

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
# Копируем конфигурационный файл
cp config.yaml debian/opt/SQLManager/config.yaml 2>/dev/null || true

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

# Создаем, если отсутствует, файл в /etc/smbcredentials/.veeamsrv_creds для выполнения команд монтирования
if [ -f /etc/smbcredentials/.veeamsrv_creds ]; then
    echo "username=user"  > /etc/smbcredentials/.veeamsrv_creds
    echo "password=password"  > /etc/smbcredentials/.veeamsrv_creds
    echo "domain=domain" > /etc/smbcredentials/.veeamsrv_creds
    chmod 640 /etc/smbcredentials/.veeamsrv_creds
fi

# Устанавливаем права на лог-файл, если он существует
if [ -f /var/log/sqlmanager/sqlmanager.log ]; then
    chmod 640 /var/log/sqlmanager/sqlmanager.log
fi
chmod 750 /var/log/sqlmanager

# Создаем, если отсутствует, файл в /etc/sudoers.d для выполнения команд монтирования
if [ -f /etc/sudoers.d/sqlmanager ]; then
    echo "mssql ALL=(ALL) NOPASSWD: /bin/systemctl start mnt-sql_backups.mount, /bin/systemctl status mnt-sql_backups.mount" > /etc/sudoers.d/sqlmanager
    chmod 440 /etc/sudoers.d/sqlmanager
fi

# Перезагружаем systemd
systemctl daemon-reload || true

# Включаем и запускаем сервисы
systemctl enable mnt-sql_backups.mount || true
# systemctl start mnt-sql_backups.mount || true
systemctl enable sqlmanager.service || true
# systemctl start sqlmanager.service || true

echo
echo "SQLManager установлен."
echo "!!! Пожалуйста, отредактируйте файлы: /etc/smbcredentials/.veeamsrv_creds и /opt/SQLManager/config.yaml, если устанавливаете приложение впервые !!!"
echo "После настройки запустите службы: sudo systemctl start mnt-sql_backups.mount и sudo systemctl start sqlmanager.service или перезагрузите сервер."
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
NEW_VERSION=$(cat .build_version)
dpkg-deb --build debian build/sqlmanager-$NEW_VERSION-amd64.deb

echo "Сборка завершена. Пакет: sqlmanager-$NEW_VERSION-amd64.deb"

# Удаляем временный файл с версией
rm -f .build_version

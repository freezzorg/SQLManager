document.addEventListener('DOMContentLoaded', () => {
    const databaseList = document.getElementById('database-list');
    const deleteDbBtn = document.getElementById('delete-db-btn');
    const refreshDbBtn = document.getElementById('refresh-db-btn');

    const backupSelect = document.getElementById('backup-select');
    const refreshBackupsBtn = document.getElementById('refresh-backups-btn');
    const newDbNameInput = document.getElementById('new-db-name');
    const clearDbNameBtn = document.getElementById('clear-db-name-btn');
    const restoreDatetimeInput = document.getElementById('restore-datetime');
    const setCurrentDatetimeBtn = document.getElementById('set-current-datetime-btn');
    const restoreDbBtn = document.getElementById('restore-db-btn');
    const confirmRestoreButtons = document.getElementById('confirm-restore-buttons');
    const confirmRestoreBtn = document.getElementById('confirm-restore-btn');
    const cancelConfirmRestoreBtn = document.getElementById('cancel-confirm-restore-btn');
    const cancelRestoreProcessBtn = document.getElementById('cancel-restore-process-btn');

    const briefLog = document.getElementById('brief-log');

    let selectedDatabase = null; // Выбранная база данных в левой панели
    let restoreInProgress = false; // Флаг для отслеживания процесса восстановления

    // --- Функции для работы с базами данных ---

    async function fetchDatabases() {
        try {
            const response = await fetch('/api/databases');
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const databases = await response.json();
            renderDatabases(databases);
        } catch (error) {
            console.error('Ошибка при получении списка баз данных:', error);
            addLogEntry(`ОШИБКА: Не удалось загрузить список баз данных: ${error.message}`);
        }
    }

    function renderDatabases(databases) {
        databaseList.innerHTML = '';
        databases.forEach(db => {
            const li = document.createElement('li');
            li.textContent = db.Name;
            li.dataset.dbName = db.Name;

            const statusIndicator = document.createElement('span');
            statusIndicator.classList.add('status-indicator');
            switch (db.State.toUpperCase()) {
                case 'ONLINE':
                    statusIndicator.classList.add('status-online');
                    break;
                case 'RESTORING':
                    statusIndicator.classList.add('status-restoring');
                    break;
                case 'OFFLINE':
                case 'SUSPECT':
                default:
                    statusIndicator.classList.add('status-offline');
                    break;
            }
            li.appendChild(statusIndicator);

            li.addEventListener('click', () => {
                // Снимаем выделение с предыдущей
                if (selectedDatabase) {
                    const prevSelected = databaseList.querySelector(`li[data-db-name="${selectedDatabase}"]`);
                    if (prevSelected) {
                        prevSelected.classList.remove('selected');
                    }
                }
                // Выделяем текущую
                li.classList.add('selected');
                selectedDatabase = db.Name;
                newDbNameInput.value = db.Name; // Копируем имя в поле ввода
            });
            databaseList.appendChild(li);
        });
    }

    async function deleteSelectedDatabase() {
        if (!selectedDatabase) {
            alert('Пожалуйста, выберите базу данных для удаления.');
            return;
        }

        if (confirm(`База данных '${selectedDatabase}' будет удалена. Продолжить?`)) {
            try {
                const response = await fetch(`/api/delete?name=${selectedDatabase}`, {
                    method: 'DELETE',
                });
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                addLogEntry(`База данных '${selectedDatabase}' успешно удалена.`);
                selectedDatabase = null; // Сбрасываем выбор
                fetchDatabases(); // Обновляем список
            } catch (error) {
                console.error('Ошибка при удалении базы данных:', error);
                addLogEntry(`ОШИБКА: Не удалось удалить базу данных '${selectedDatabase}': ${error.message}`);
            }
        }
    }

    // --- Функции для работы с бэкапами ---

    async function fetchBackups() {
        try {
            const response = await fetch('/api/backups');
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const backups = await response.json();
            renderBackups(backups);
        } catch (error) {
            console.error('Ошибка при получении списка бэкапов:', error);
            addLogEntry(`ОШИБКА: Не удалось загрузить список бэкапов: ${error.message}`);
        }
    }

    function renderBackups(backups) {
        backupSelect.innerHTML = '';
        if (backups.length === 0) {
            const option = document.createElement('option');
            option.value = '';
            option.textContent = 'Бэкапы не найдены';
            backupSelect.appendChild(option);
            return;
        }
        backups.forEach(backup => {
            const option = document.createElement('option');
            option.value = backup.FileName; // Полный путь к файлу бэкапа
            option.textContent = `${backup.FileName} (${new Date(backup.BackupDate).toLocaleString()})`;
            backupSelect.appendChild(option);
        });
    }

    // --- Функции для восстановления ---

    function setRestoreButtonsState(state) {
        if (state === 'initial') {
            restoreDbBtn.style.display = 'block';
            confirmRestoreButtons.style.display = 'none';
            cancelRestoreProcessBtn.style.display = 'none';
            restoreInProgress = false;
        } else if (state === 'confirm') {
            restoreDbBtn.style.display = 'none';
            confirmRestoreButtons.style.display = 'flex';
            cancelRestoreProcessBtn.style.display = 'none';
            restoreInProgress = false;
        } else if (state === 'restoring') {
            restoreDbBtn.style.display = 'none';
            confirmRestoreButtons.style.display = 'none';
            cancelRestoreProcessBtn.style.display = 'block';
            restoreInProgress = true;
        }
    }

    async function startRestoreProcess() {
        const backupPath = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();
        let restoreTime = restoreDatetimeInput.value;

        if (!backupPath) {
            alert('Пожалуйста, выберите файл бэкапа.');
            return;
        }
        if (!newDbName) {
            alert('Пожалуйста, введите имя восстанавливаемой базы данных.');
            return;
        }

        // Форматируем дату/время для Go-бэкенда
        if (restoreTime) {
            const dt = new Date(restoreTime);
            restoreTime = `${dt.getDate().toString().padStart(2, '0')}.${(dt.getMonth() + 1).toString().padStart(2, '0')}.${dt.getFullYear()} ${dt.getHours().toString().padStart(2, '0')}:${dt.getMinutes().toString().padStart(2, '0')}:${dt.getSeconds().toString().padStart(2, '0')}`;
        }

        try {
            const response = await fetch('/api/restore', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    backupPath: backupPath,
                    newDbName: newDbName,
                    restoreTime: restoreTime,
                }),
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP error! status: ${response.status}, message: ${errorText}`);
            }

            addLogEntry(`Запрос на восстановление базы '${newDbName}' принят.`);
            setRestoreButtonsState('restoring');
            fetchDatabases(); // Обновляем список баз, чтобы увидеть состояние RESTORING
        } catch (error) {
            console.error('Ошибка при запуске восстановления:', error);
            addLogEntry(`ОШИБКА: Не удалось запустить восстановление базы '${newDbName}': ${error.message}`);
            setRestoreButtonsState('initial'); // Возвращаем кнопки в исходное состояние
        }
    }

    async function cancelRestore() {
        if (!restoreInProgress) {
            alert('Нет активного процесса восстановления для отмены.');
            return;
        }

        const dbName = newDbNameInput.value.trim();
        if (!dbName) {
            alert('Имя восстанавливаемой базы данных не указано.');
            return;
        }

        if (confirm(`Восстанавливаемая база '${dbName}' станет нерабочей и будет удалена. Хотите отменить восстановление?`)) {
            try {
                const response = await fetch(`/api/cancel-restore?name=${dbName}`, {
                    method: 'DELETE',
                });
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                addLogEntry(`Восстановление базы данных '${dbName}' отменено (БД удалена).`);
                setRestoreButtonsState('initial');
                fetchDatabases(); // Обновляем список баз
            } catch (error) {
                console.error('Ошибка при отмене восстановления:', error);
                addLogEntry(`ОШИБКА: Не удалось отменить восстановление базы '${dbName}': ${error.message}`);
            }
        }
    }

    // --- Функции для лога ---

    async function fetchBriefLog() {
        try {
            const response = await fetch('/api/log');
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const logEntries = await response.json();
            renderBriefLog(logEntries);
        } catch (error) {
            console.error('Ошибка при получении краткого лога:', error);
            // Не добавляем в лог, чтобы избежать рекурсии
        }
    }

    function renderBriefLog(logEntries) {
        briefLog.innerHTML = '';
        logEntries.forEach(entry => {
            const li = document.createElement('li');
            const timestamp = new Date(entry.Timestamp).toLocaleString();
            li.textContent = `${timestamp} ${entry.Message}`;
            briefLog.appendChild(li);
        });
        briefLog.scrollTop = briefLog.scrollHeight; // Прокрутка вниз
    }

    function addLogEntry(message) {
        // Добавляем запись в краткий лог на клиенте
        const li = document.createElement('li');
        const timestamp = new Date().toLocaleString();
        li.textContent = `${timestamp} ${message}`;
        briefLog.appendChild(li);
        briefLog.scrollTop = briefLog.scrollHeight; // Прокрутка вниз
    }

    // --- Обработчики событий ---

    refreshDbBtn.addEventListener('click', fetchDatabases);
    deleteDbBtn.addEventListener('click', deleteSelectedDatabase);

    refreshBackupsBtn.addEventListener('click', fetchBackups);
    clearDbNameBtn.addEventListener('click', () => {
        newDbNameInput.value = '';
    });
    setCurrentDatetimeBtn.addEventListener('click', () => {
        const now = new Date();
        const year = now.getFullYear();
        const month = (now.getMonth() + 1).toString().padStart(2, '0');
        const day = now.getDate().toString().padStart(2, '0');
        const hours = now.getHours().toString().padStart(2, '0');
        const minutes = now.getMinutes().toString().padStart(2, '0');
        // datetime-local не поддерживает секунды, поэтому обрезаем
        restoreDatetimeInput.value = `${year}-${month}-${day}T${hours}:${minutes}`;
    });

    restoreDbBtn.addEventListener('click', async () => {
        const newDbName = newDbNameInput.value.trim();
        if (!newDbName) {
            alert('Пожалуйста, введите имя восстанавливаемой базы данных.');
            return;
        }

        // Проверяем, существует ли база данных на сервере
        try {
            const response = await fetch('/api/databases');
            const databases = await response.json();
            const dbExists = databases.some(db => db.Name === newDbName);

            if (dbExists) {
                if (confirm(`Вы действительно хотите восстановить бэкап в существующую базу '${newDbName}'?`)) {
                    setRestoreButtonsState('confirm');
                } else {
                    setRestoreButtonsState('initial');
                }
            } else {
                startRestoreProcess();
            }
        } catch (error) {
            console.error('Ошибка при проверке существования БД перед восстановлением:', error);
            addLogEntry(`ОШИБКА: Не удалось проверить существование БД: ${error.message}`);
            setRestoreButtonsState('initial');
        }
    });

    confirmRestoreBtn.addEventListener('click', startRestoreProcess);
    cancelConfirmRestoreBtn.addEventListener('click', () => {
        setRestoreButtonsState('initial');
    });
    cancelRestoreProcessBtn.addEventListener('click', cancelRestore);

    // --- Инициализация ---
    fetchDatabases();
    fetchBackups();
    fetchBriefLog();
    setInterval(fetchBriefLog, 5000); // Обновляем лог каждые 5 секунд
});

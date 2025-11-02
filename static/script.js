document.addEventListener('DOMContentLoaded', () => {
    const databaseList = document.getElementById('database-list');
    const deleteDbBtn = document.getElementById('delete-db-btn');
    const refreshDbBtn = document.getElementById('refresh-db-btn');
    const backupDbBtn = document.getElementById('backup-db-btn'); // Добавляем ссылку на кнопку "Бэкап"

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

    const briefLog = document.getElementById('brief-log');
    const restoreForm = document.getElementById('restore-form'); // Добавляем ссылку на форму
    const confirmationSection = document.getElementById('confirmation-section'); // Добавляем ссылку на секцию подтверждения

    let selectedDatabase = null;
    // let restoreInProgress = false; // Больше не нужна, статус будет в restoreProgresses

    const restoreProgressPollingInterval = 3000; // Интервал опроса прогресса в мс
    const activeRestorePollers = {}; // Хранит setInterval ID для каждой восстанавливаемой БД
    const activeBackupPollers = {}; // Хранит setInterval ID для каждой бэкапируемой БД

    // --- Утилиты ---

    // --- Утилиты ---

    // Универсальная функция форматирования даты/времени
    const formatDateTime = (date, type = 'input') => {
        const d = String(date.getDate()).padStart(2, '0');
        const m = String(date.getMonth() + 1).padStart(2, '0');
        const y = date.getFullYear();
        const h = String(date.getHours()).padStart(2, '0');
        const min = String(date.getMinutes()).padStart(2, '0');
        const s = String(date.getSeconds()).padStart(2, '0');

        if (type === 'input') {
            return `${y}-${m}-${d}T${h}:${min}:${s}`;
        } else if (type === 'backend') {
            return `${y}-${m}-${d} ${h}:${min}:${s}`;
        } else if (type === 'log') {
            return `${d}.${m}.${y} ${h}:${min}:${s}`;
        }
        return '';
    };

    const addLogEntry = (message) => {
        console.log(message);
        const li = document.createElement('li');
        const time = formatDateTime(new Date(), 'log');
        li.textContent = `${time} ${message}`;
        briefLog.prepend(li);
        while (briefLog.children.length > 100) {
            briefLog.removeChild(briefLog.lastChild);
        }
    };

    const fetchBriefLog = async () => {
        try {
            const response = await fetch('/api/log');
            if (!response.ok) throw new Error('Ошибка получения лога');
            const logEntries = await response.json();

            briefLog.innerHTML = '';
            logEntries.reverse().forEach(entry => {
                const li = document.createElement('li');
                const time = formatDateTime(new Date(entry.timestamp), 'log');
                li.textContent = `${time} ${entry.message}`;
                briefLog.appendChild(li);
            });
        } catch (error) {
            console.error('Ошибка получения краткого лога:', error);
        }
    };

    // Вспомогательные функции для создания/обновления прогресс-контейнеров
    const createRestoreProgressContainer = (dbName) => {
        const container = document.createElement('div');
        container.className = 'restore-progress-container';
        container.innerHTML = `
            <div class="progress-bar">
                <div class="progress-fill" style="width: 0%;"></div>
            </div>
            <span class="progress-text">0%</span>
            <button class="cancel-restore-btn-inline" data-dbname="${dbName}" aria-label="Отменить восстановление ${dbName}">Отменить</button>
        `;
        container.querySelector('.cancel-restore-btn-inline').addEventListener('click', (event) => {
            event.stopPropagation();
            cancelRestore(dbName);
        });
        return container;
    };

    const createBackupProgressContainer = (dbName) => {
        const container = document.createElement('div');
        container.className = 'backup-progress-container';
        container.innerHTML = `
            <div class="progress-bar">
                <div class="progress-fill" style="width: 0%;"></div>
            </div>
            <span class="progress-text">0%</span>
        `;
        return container;
    };

    // --- API Функции ---

    const fetchDatabases = async () => {
        try {
            const response = await fetch('/api/databases');
            if (!response.ok) {
                throw new Error(`Ошибка сервера: ${response.status}`);
            }
            const databases = await response.json();

            databaseList.innerHTML = '';

            const restoringDbs = [];
            const backingUpDbs = [];

            databases.forEach(db => {
                const li = document.createElement('li');
                li.dataset.dbname = db.name;
                li.setAttribute('role', 'option');
                li.setAttribute('tabindex', '0'); // Делаем элемент фокусируемым
                li.addEventListener('click', () => {
                    selectedDatabase = db.name;
                    document.querySelectorAll('.db-item').forEach(item => {
                        item.classList.remove('selected');
                        item.removeAttribute('aria-selected');
                    });
                    li.classList.add('selected');
                    li.setAttribute('aria-selected', 'true');
                });
                li.addEventListener('dblclick', () => {
                    newDbNameInput.value = db.name;
                });
                databaseList.appendChild(li);

                li.className = `db-item db-state-${db.state}`;
                
                const dbNameSpan = document.createElement('span');
                dbNameSpan.className = 'db-name';
                dbNameSpan.textContent = db.name;
                li.prepend(dbNameSpan);

                const statusIconSpan = document.createElement('span');
                statusIconSpan.className = 'db-status-icon';
                li.insertBefore(statusIconSpan, dbNameSpan.nextSibling);
                statusIconSpan.title = db.state;
                let iconClass = '';
                switch (db.state) {
                    case 'online':
                        iconClass = 'fas fa-check-circle online';
                        break;
                    case 'restoring':
                        iconClass = 'fas fa-sync-alt fa-spin restoring';
                        restoringDbs.push(db.name);
                        break;
                    case 'backing_up':
                        iconClass = 'fas fa-save fa-spin backing-up';
                        backingUpDbs.push(db.name);
                        break;
                    case 'offline':
                        iconClass = 'fas fa-power-off offline';
                        break;
                    case 'error':
                        iconClass = 'fas fa-exclamation-triangle error';
                        break;
                    default:
                        iconClass = 'fas fa-question-circle unknown';
                }
                statusIconSpan.innerHTML = `<i class="${iconClass}"></i>`;

                let restoreProgressContainer = li.querySelector('.restore-progress-container');
                if (db.state === 'restoring') {
                    if (!restoreProgressContainer) {
                        restoreProgressContainer = createRestoreProgressContainer(db.name);
                        li.appendChild(restoreProgressContainer);
                    }
                    restoreProgressContainer.style.display = 'flex';
                    startRestoreProgressPolling(db.name);
                } else {
                    if (restoreProgressContainer) {
                        restoreProgressContainer.style.display = 'none';
                    }
                }

                let backupProgressContainer = li.querySelector('.backup-progress-container');
                if (db.state === 'backing_up') {
                    if (!backupProgressContainer) {
                        backupProgressContainer = createBackupProgressContainer(db.name);
                        li.appendChild(backupProgressContainer);
                    }
                    backupProgressContainer.style.display = 'flex';
                    startBackupProgressPolling(db.name);
                } else {
                    if (backupProgressContainer) {
                        backupProgressContainer.style.display = 'none';
                    }
                }
            });

            Object.keys(activeRestorePollers).forEach(dbName => {
                if (!restoringDbs.includes(dbName)) {
                    clearInterval(activeRestorePollers[dbName]);
                    delete activeRestorePollers[dbName];
                    const li = databaseList.querySelector(`li[data-dbname="${dbName}"]`);
                    if (li) {
                        const progressContainer = li.querySelector('.restore-progress-container');
                        if (progressContainer) progressContainer.style.display = 'none';
                    }
                }
            });

            Object.keys(activeBackupPollers).forEach(dbName => {
                if (!backingUpDbs.includes(dbName)) {
                    clearInterval(activeBackupPollers[dbName]);
                    delete activeBackupPollers[dbName];
                    const li = databaseList.querySelector(`li[data-dbname="${dbName}"]`);
                    if (li) {
                        const progressContainer = li.querySelector('.backup-progress-container');
                        if (progressContainer) progressContainer.style.display = 'none';
                    }
                }
            });

        } catch (error) {
            console.error('Ошибка получения списка баз данных:', error);
            addLogEntry(`ОШИБКА: Не удалось получить список баз данных: ${error.message}`);
        }
    };

    const fetchBackups = async () => {
        try {
            const response = await fetch('/api/backups');
            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(errorText);
            }
            const backups = await response.json();

            backupSelect.innerHTML = '<option value="" disabled selected>Выберите бэкап</option>';
            backups.forEach(backup => {
                const option = document.createElement('option');
                option.value = backup.baseName;
                option.textContent = backup.baseName;
                backupSelect.appendChild(option);
            });
        } catch (error) {
            console.error('Ошибка получения списка бэкапов:', error);
            addLogEntry(`ОШИБКА: Не удалось получить список бэкапов: ${error.message}`);
        }
    };

    const deleteDatabase = async (dbName) => {
        if (!dbName) {
            alert('Пожалуйста, выберите базу данных для удаления.');
            return;
        }
        if (!confirm(`Вы действительно хотите удалить базу данных '${dbName}'?`)) {
            return;
        }

        try {
            const response = await fetch(`/api/delete?name=${encodeURIComponent(dbName)}`, {
                method: 'DELETE',
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }

            const result = await response.json();
            addLogEntry(`Удаление базы '${dbName}' успешно завершено`);
            selectedDatabase = null;
            fetchDatabases();
        } catch (error) {
            console.error('Ошибка удаления базы данных:', error);
            addLogEntry(`ОШИБКА: Не удалось удалить базу данных '${dbName}': ${error.message}`);
        }
    };

    async function startRestoreProcess(confirmOverwrite = false) {
        const backupBaseName = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();
        let restoreDateTime = restoreDatetimeInput.value.trim();
        let formattedDateTime = "";

        if (!backupBaseName || !newDbName) {
            alert('Пожалуйста, выберите директорию бэкапа и введите имя восстанавливаемой базы данных.');
            return;
        }

        if (restoreDateTime !== "") {
            try {
                const dateObj = new Date(restoreDateTime);
                if (isNaN(dateObj.getTime())) {
                    throw new Error("Некорректный формат даты/времени");
                }
                formattedDateTime = formatDateTime(dateObj, 'backend');

            } catch (e) {
                console.error("Ошибка форматирования даты:", e);
                addLogEntry(`ОШИБКА: Не удалось обработать дату/время: ${restoreDateTime}. Убедитесь, что формат: ГГГГ-ММ-ДДТЧЧ:ММ:СС`);
                return;
            }
        }

        addLogEntry(`Начато восстановление базы '${newDbName}' из бэкапа '${backupBaseName}'${formattedDateTime ? ` на ${formattedDateTime}` : ''}`);

        const requestBody = {
            backupBaseName: backupBaseName,
            newDbName: newDbName,
            restoreDateTime: formattedDateTime,
        };

        try {
            const response = await fetch('/api/restore', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(requestBody)
            });

            if (response.ok) {
                startRestoreProgressPolling(newDbName);
                fetchDatabases();
            } else {
                const errorText = await response.text();
                addLogEntry(`ОШИБКА: Не удалось запустить восстановление базы '${newDbName}'. Сервер вернул: ${response.status} - ${errorText}`);
                console.error('Ошибка восстановления:', errorText);
                fetchDatabases();
            }
        } catch (error) {
            addLogEntry(`КРИТИЧЕСКАЯ ОШИБКА: Проблема с сетевым запросом при восстановлении базы '${newDbName}': ${error.message}`);
            console.error('Сетевая ошибка:', error);
            fetchDatabases();
        }
    }

    const cancelRestore = async (dbName) => {
        if (!dbName) {
            alert('Невозможно отменить: имя восстанавливаемой базы не определено.');
            fetchDatabases();
            return;
        }

        if (!confirm(`Восстанавливаемая база станет не рабочей и будет удалена. Хотите отменить восстановление базы данных '${dbName}'?`)) {
            return;
        }

        addLogEntry(`Отмена восстановления базы данных '${dbName}'...`);

        try {
            const response = await fetch(`/api/cancel-restore?name=${encodeURIComponent(dbName)}`, {
                method: 'POST',
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }

            const result = await response.json();
            addLogEntry(`Отмена восстановления базы '${dbName}' успешно завершена`);
            if (activeRestorePollers[dbName]) {
                clearInterval(activeRestorePollers[dbName]);
                delete activeRestorePollers[dbName];
            }

        } catch (error) {
            console.error('Ошибка отмены восстановления:', error);
            addLogEntry(`ОШИБКА: Не удалось отменить восстановление: ${error.message}`);
        } finally {
            fetchDatabases();
        }
    };

    const startBackupProcess = async (dbName) => {
        if (!dbName) {
            alert('Пожалуйста, выберите базу данных для бэкапа.');
            return;
        }

        addLogEntry(`Начато создание бэкапа базы '${dbName}'...`);

        try {
            const response = await fetch('/api/backup', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ dbName: dbName })
            });

            if (response.ok) {
                startBackupProgressPolling(dbName);
                fetchDatabases();
            } else {
                const errorText = await response.text();
                addLogEntry(`ОШИБКА: Не удалось запустить создание бэкапа базы '${dbName}'. Сервер вернул: ${response.status} - ${errorText}`);
                console.error('Ошибка бэкапа:', errorText);
                fetchDatabases();
            }
        } catch (error) {
            addLogEntry(`КРИТИЧЕСКАЯ ОШИБКА: Проблема с сетевым запросом при создании бэкапа базы '${dbName}': ${error.message}`);
            console.error('Сетевая ошибка:', error);
            fetchDatabases();
        }
    };


    // --- Обработчики событий ---

    refreshDbBtn.addEventListener('click', fetchDatabases);
    deleteDbBtn.addEventListener('click', () => deleteDatabase(selectedDatabase));
    backupDbBtn.addEventListener('click', () => {
        if (selectedDatabase) {
            startBackupProcess(selectedDatabase);
        } else {
            alert('Пожалуйста, выберите базу данных для создания бэкапа.');
        }
    });
    refreshBackupsBtn.addEventListener('click', fetchBackups);

    clearDbNameBtn.addEventListener('click', () => {
        newDbNameInput.value = '';
    });

    setCurrentDatetimeBtn.addEventListener('click', () => {
        const now = new Date();
        restoreDatetimeInput.value = formatDateTime(now, 'input');
        restoreDatetimeInput.focus();
    });

    // Обработчик отправки формы
    restoreForm.addEventListener('submit', async (event) => {
        event.preventDefault();

        const backupBaseName = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();

        if (!backupBaseName || !newDbName) {
            alert('Пожалуйста, выберите бэкап и введите имя восстанавливаемой базы данных.');
            return;
        }

        try {
            const response = await fetch('/api/databases');
            const databases = await response.json();
            const dbExists = databases.some(db => db.name.toLowerCase() === newDbName.toLowerCase());

            if (dbExists) {
                confirmRestoreButtons.style.display = 'flex';
                confirmationSection.style.display = 'block';
                restoreDbBtn.style.display = 'none';
            } else {
                startRestoreProcess();
                confirmRestoreButtons.style.display = 'none';
                confirmationSection.style.display = 'none';
                restoreDbBtn.style.display = 'block';
            }
        } catch (error) {
            console.error('Ошибка при проверке существования БД перед восстановлением:', error);
            addLogEntry(`ОШИБКА: Не удалось проверить существование БД '${newDbName}' перед восстановлением: ${error.message}`);
            confirmRestoreButtons.style.display = 'none';
            confirmationSection.style.display = 'none';
            restoreDbBtn.style.display = 'block';
        }
    });

    confirmRestoreBtn.addEventListener('click', () => {
        startRestoreProcess();
        confirmRestoreButtons.style.display = 'none';
        confirmationSection.style.display = 'none';
        restoreDbBtn.style.display = 'block';
    });
    cancelConfirmRestoreBtn.addEventListener('click', () => {
        confirmRestoreButtons.style.display = 'none';
        confirmationSection.style.display = 'none';
        restoreDbBtn.style.display = 'block';
    });

    // Обработчик для кнопок отмены внутри прогресс-баров
    databaseList.addEventListener('click', (event) => {
        if (event.target.classList.contains('cancel-restore-btn-inline')) {
            const dbName = event.target.dataset.dbname;
            cancelRestore(dbName);
        }
    });

    // --- Новые функции для прогресса восстановления ---

    const startRestoreProgressPolling = (dbName) => {
        if (activeRestorePollers[dbName]) {
            return;
        }
        activeRestorePollers[dbName] = setInterval(() => fetchRestoreProgress(dbName), restoreProgressPollingInterval);
    };

    const fetchRestoreProgress = async (dbName) => {
        try {
            const response = await fetch(`/api/restore-progress?name=${encodeURIComponent(dbName)}`);
            if (!response.ok) {
                throw new Error(`Ошибка получения прогресса для ${dbName}`);
            }
            const progress = await response.json();

            updateRestoreProgressDisplay(dbName, progress);

            if (progress.status === 'completed' || progress.status === 'failed' || progress.status === 'not_found') {
                let statusMessage = '';
                switch (progress.status) {
                    case 'completed':
                        statusMessage = 'успешно завершено';
                        break;
                    case 'failed':
                        statusMessage = 'завершено с ошибкой';
                        break;
                    case 'not_found':
                        statusMessage = 'не найдено (возможно, уже завершено)';
                        break;
                }
                addLogEntry(`Восстановление базы '${dbName}' ${statusMessage}`);
                if (activeRestorePollers[dbName]) {
                    clearInterval(activeRestorePollers[dbName]);
                    delete activeRestorePollers[dbName];
                }
                fetchDatabases();
            }

        } catch (error) {
            console.error(`Ошибка получения прогресса для ${dbName}:`, error);
            addLogEntry(`ОШИБКА: Не удалось получить прогресс для базы '${dbName}': ${error.message}`);
            if (activeRestorePollers[dbName]) {
                clearInterval(activeRestorePollers[dbName]);
                delete activeRestorePollers[dbName];
            }
            fetchDatabases();
        }
    };

    const updateRestoreProgressDisplay = (dbName, progress) => {
        const li = databaseList.querySelector(`li[data-dbname="${dbName}"]`);
        if (!li) return;

        const statusIconSpan = li.querySelector('.db-status-icon');
        const progressContainer = li.querySelector('.restore-progress-container');
        const progressBarFill = li.querySelector('.progress-fill');
        const progressText = li.querySelector('.progress-text');

        if (progress.status === 'in_progress' || progress.status === 'pending') {
            progressContainer.style.display = 'flex';
            progressBarFill.style.width = `${progress.percentage}%`;
            progressText.textContent = `${progress.percentage}%`;
            statusIconSpan.innerHTML = `<i class="fas fa-sync-alt fa-spin restoring" title="Восстанавливается"></i>`;
            statusIconSpan.title = "restoring";
        } else {
            progressContainer.style.display = 'none';
            let iconClass = '';
            let title = '';
            switch (progress.status) {
                case 'completed':
                    iconClass = 'fas fa-check-circle online';
                    title = 'Online';
                    break;
                case 'failed':
                    iconClass = 'fas fa-exclamation-triangle error';
                    title = 'Ошибка восстановления';
                    break;
                case 'cancelled':
                    iconClass = 'fas fa-times-circle offline';
                    title = 'Восстановление отменено';
                    break;
                case 'not_found':
                    return;
                default:
                    iconClass = 'fas fa-question-circle unknown';
                    title = 'Неизвестный статус';
            }
            statusIconSpan.innerHTML = `<i class="${iconClass}"></i>`;
            statusIconSpan.title = title;
        }
    };

    // --- Новые функции для прогресса бэкапа ---

    const startBackupProgressPolling = (dbName) => {
        if (activeBackupPollers[dbName]) {
            return;
        }
        activeBackupPollers[dbName] = setInterval(() => fetchBackupProgress(dbName), restoreProgressPollingInterval);
    };

    const fetchBackupProgress = async (dbName) => {
        try {
            const response = await fetch(`/api/backup-progress?name=${encodeURIComponent(dbName)}`);
            if (!response.ok) {
                throw new Error(`Ошибка получения прогресса бэкапа для ${dbName}`);
            }
            const progress = await response.json();

            updateBackupProgressDisplay(dbName, progress);

            if (progress.status === 'completed' || progress.status === 'failed' || progress.status === 'not_found') {
                let statusMessage = '';
                switch (progress.status) {
                    case 'completed':
                        statusMessage = 'успешно завершено';
                        break;
                    case 'failed':
                        statusMessage = 'завершено с ошибкой';
                        break;
                    case 'not_found':
                        statusMessage = 'не найдено (возможно, уже завершено)';
                        break;
                }
                addLogEntry(`Создание бэкапа базы '${dbName}' ${statusMessage}`);
                if (activeBackupPollers[dbName]) {
                    clearInterval(activeBackupPollers[dbName]);
                    delete activeBackupPollers[dbName];
                }
                fetchDatabases();
            }

        } catch (error) {
            console.error(`Ошибка получения прогресса бэкапа для ${dbName}:`, error);
            addLogEntry(`ОШИБКА: Не удалось получить прогресс бэкапа для базы '${dbName}': ${error.message}`);
            if (activeBackupPollers[dbName]) {
                clearInterval(activeBackupPollers[dbName]);
                delete activeBackupPollers[dbName];
            }
            fetchDatabases();
        }
    };

    const updateBackupProgressDisplay = (dbName, progress) => {
        const li = databaseList.querySelector(`li[data-dbname="${dbName}"]`);
        if (!li) return;

        const statusIconSpan = li.querySelector('.db-status-icon');
        const progressContainer = li.querySelector('.backup-progress-container');
        const progressBarFill = li.querySelector('.progress-fill');
        const progressText = li.querySelector('.progress-text');

        if (progress.status === 'in_progress' || progress.status === 'pending') {
            progressContainer.style.display = 'flex';
            progressBarFill.style.width = `${progress.percentage}%`;
            progressText.textContent = `${progress.percentage}%`;
            statusIconSpan.innerHTML = `<i class="fas fa-save fa-spin backing-up" title="Создается бэкап"></i>`;
            statusIconSpan.title = "backing_up";
        } else {
            progressContainer.style.display = 'none';
            let iconClass = '';
            let title = '';
            switch (progress.status) {
                case 'completed':
                    iconClass = 'fas fa-check-circle online';
                    title = 'Online';
                    break;
                case 'failed':
                    iconClass = 'fas fa-exclamation-triangle error';
                    title = 'Ошибка бэкапа';
                    break;
                case 'not_found':
                    return;
                default:
                    iconClass = 'fas fa-question-circle unknown';
                    title = 'Неизвестный статус';
            }
            statusIconSpan.innerHTML = `<i class="${iconClass}"></i>`;
            statusIconSpan.title = title;
        }
    };

    // --- Инициализация ---
    fetchDatabases();
    fetchBackups();
    fetchBriefLog();
});

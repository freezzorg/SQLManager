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
    const restoreForm = document.getElementById('restore-form'); // Добавляем ссылку на форму
    const confirmationSection = document.getElementById('confirmation-section'); // Добавляем ссылку на секцию подтверждения

    let selectedDatabase = null;
    // let restoreInProgress = false; // Больше не нужна, статус будет в restoreProgresses

    const restoreProgressPollingInterval = 3000; // Интервал опроса прогресса в мс
    const activeRestorePollers = {}; // Хранит setInterval ID для каждой восстанавливаемой БД

    // --- Утилиты ---

    const addLogEntry = (message) => {
        console.log(message);
        const li = document.createElement('li');
        const time = new Date().toLocaleTimeString('ru-RU');
        li.textContent = `[${time}] ${message}`;
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
                const time = new Date(entry.timestamp).toLocaleTimeString('ru-RU');
                li.textContent = `[${time}] ${entry.message}`;
                briefLog.appendChild(li);
            });
        } catch (error) {
            console.error('Ошибка получения краткого лога:', error);
        }
    };

    // setRestoreButtonsState больше не нужна в таком виде, логика кнопок будет управляться через состояние БД
    // и наличие активных процессов восстановления.
    // Кнопка отмены восстановления будет привязана к конкретной БД.
    // setRestoreButtonsState будет удалена.

    function formatDateToInput(date) {
        const y = date.getFullYear();
        const m = String(date.getMonth() + 1).padStart(2, '0');
        const d = String(date.getDate()).padStart(2, '0');
        const h = String(date.getHours()).padStart(2, '0');
        const min = String(date.getMinutes()).padStart(2, '0');
        const s = String(date.getSeconds()).padStart(2, '0');
        return `${y}-${m}-${d}T${h}:${min}:${s}`;
    }

    function formatDateToBackend(date) {
        const y = date.getFullYear();
        const m = String(date.getMonth() + 1).padStart(2, '0');
        const d = String(date.getDate()).padStart(2, '0');
        const h = String(date.getHours()).padStart(2, '0');
        const min = String(date.getMinutes()).padStart(2, '0');
        const s = String(date.getSeconds()).padStart(2, '0');
        return `${y}-${m}-${d} ${h}:${min}:${s}`;
    }

    // --- API Функции ---

    const fetchDatabases = async () => {
        try {
            const response = await fetch('/api/databases');
            const databases = await response.json();

            const currentDbElements = new Map();
            databaseList.querySelectorAll('li.db-item').forEach(li => {
                currentDbElements.set(li.dataset.dbname, li);
            });

            const restoringDbs = []; // Для отслеживания баз в процессе восстановления
            const newDbNames = new Set(databases.map(db => db.name));

            databases.forEach(db => {
                let li = currentDbElements.get(db.name);

                if (!li) {
                    // Если элемента нет, создаем новый
                    li = document.createElement('li');
                    li.dataset.dbname = db.name;
                    li.addEventListener('click', () => {
                        selectedDatabase = db.name;
                        newDbNameInput.value = db.name;

                        document.querySelectorAll('.db-item').forEach(item => {
                            item.classList.remove('selected');
                        });
                        li.classList.add('selected');
                    });
                    databaseList.appendChild(li); // Добавляем новый элемент в список
                } else {
                    currentDbElements.delete(db.name); // Удаляем из карты, чтобы отслеживать удаленные
                }

                // Обновляем классы и содержимое
                li.className = `db-item db-state-${db.state}`;
                
                let dbNameSpan = li.querySelector('.db-name');
                if (!dbNameSpan) {
                    dbNameSpan = document.createElement('span');
                    dbNameSpan.className = 'db-name';
                    li.prepend(dbNameSpan); // Добавляем в начало
                }
                dbNameSpan.textContent = db.name;

                let statusIconSpan = li.querySelector('.db-status-icon');
                if (!statusIconSpan) {
                    statusIconSpan = document.createElement('span');
                    statusIconSpan.className = 'db-status-icon';
                    li.insertBefore(statusIconSpan, dbNameSpan.nextSibling); // Добавляем после имени
                }
                statusIconSpan.title = db.state;
                let iconClass = '';
                switch (db.state) {
                    case 'online':
                        iconClass = 'fas fa-check-circle online';
                        break;
                    case 'restoring':
                        iconClass = 'fas fa-sync-alt fa-spin restoring'; // Вращающаяся иконка для восстановления
                        restoringDbs.push(db.name); // Добавляем в список восстанавливаемых
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

                let progressContainer = li.querySelector('.restore-progress-container');
                if (db.state === 'restoring') {
                    if (!progressContainer) {
                        progressContainer = document.createElement('div');
                        progressContainer.className = 'restore-progress-container';
                        progressContainer.innerHTML = `
                            <div class="progress-bar">
                                <div class="progress-fill" style="width: 0%;"></div>
                            </div>
                            <span class="progress-text">0%</span>
                            <button class="cancel-restore-btn-inline" data-dbname="${db.name}">Отменить</button>
                        `;
                        li.appendChild(progressContainer);
                        // Добавляем обработчик для новой кнопки отмены
                        progressContainer.querySelector('.cancel-restore-btn-inline').addEventListener('click', (event) => {
                            event.stopPropagation(); // Предотвращаем всплытие события на li
                            cancelRestore(db.name);
                        });
                    }
                    progressContainer.style.display = 'flex'; // Показываем прогресс-бар
                    startRestoreProgressPolling(db.name); // Убеждаемся, что опрос запущен
                } else {
                    if (progressContainer) {
                        progressContainer.style.display = 'none'; // Скрываем прогресс-бар
                    }
                }
            });

            // Удаляем элементы, которых больше нет в списке баз данных
            currentDbElements.forEach(li => {
                databaseList.removeChild(li);
                // Останавливаем опрос, если он был активен для удаленной БД
                if (activeRestorePollers[li.dataset.dbname]) {
                    clearInterval(activeRestorePollers[li.dataset.dbname]);
                    delete activeRestorePollers[li.dataset.dbname];
                }
            });

            // Запускаем/останавливаем опрос прогресса для восстанавливаемых баз
            Object.keys(activeRestorePollers).forEach(dbName => {
                if (!restoringDbs.includes(dbName)) {
                    // Если база больше не восстанавливается, останавливаем опрос
                    clearInterval(activeRestorePollers[dbName]);
                    delete activeRestorePollers[dbName];
                    // Скрываем прогресс-бар, если он был виден
                    const li = databaseList.querySelector(`li[data-dbname="${dbName}"]`);
                    if (li) {
                        const progressContainer = li.querySelector('.restore-progress-container');
                        if (progressContainer) progressContainer.style.display = 'none';
                    }
                }
            });

            // Обновляем состояние главной кнопки отмены, если есть активные восстановления
            if (restoringDbs.length > 0) {
                cancelRestoreProcessBtn.style.display = 'block';
                if (restoringDbs.includes(newDbNameInput.value.trim())) {
                    cancelRestoreProcessBtn.dataset.dbname = newDbNameInput.value.trim();
                } else {
                    cancelRestoreProcessBtn.dataset.dbname = restoringDbs[0];
                }
            } else {
                cancelRestoreProcessBtn.style.display = 'none';
            }


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

    const deleteDatabase = async (dbName) => { // Принимаем dbName как аргумент
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
            addLogEntry(`УСПЕХ: ${result.message}`);
            selectedDatabase = null;
            fetchDatabases();
        } catch (error) {
            console.error('Ошибка удаления базы данных:', error);
            addLogEntry(`ОШИБКА: Не удалось удалить базу данных: ${error.message}`);
        }
    };

    async function startRestoreProcess(confirmOverwrite = false) {
        const backupBaseName = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();
        let restoreDateTime = restoreDatetimeInput.value.trim();
        let formattedDateTime = "";

        // if (restoreInProgress) { // Удаляем
        //     addLogEntry("Предупреждение: Процесс восстановления уже запущен.");
        //     return;
        // }

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
                formattedDateTime = formatDateToBackend(dateObj);

                addLogEntry(`Восстановление на момент времени (PIRT) будет выполнено до: ${formattedDateTime}`);

            } catch (e) {
                console.error("Ошибка форматирования даты:", e);
                addLogEntry(`ОШИБКА: Не удалось обработать дату/время: ${restoreDateTime}. Убедитесь, что формат: ГГГГ-ММ-ДДТЧЧ:ММ:СС`);
                return;
            }
        }

        addLogEntry(`Запуск восстановления базы данных '${newDbName}' из бэкапа '${backupBaseName}'...`);
        // setRestoreButtonsState('in_progress'); // Удаляем, так как состояние будет управляться через fetchDatabases

        const requestBody = {
            backupBaseName: backupBaseName,
            newDbName: newDbName,
            restoreDateTime: formattedDateTime,
            // confirmOverwrite: confirmOverwrite // Этот флаг не используется в бэкенде, удаляем
        };

        try {
            // restoreInProgress = true; // Удаляем
            const response = await fetch('/api/restore', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(requestBody)
            });

            if (response.ok) {
                addLogEntry(`УСПЕХ: Запрос на восстановление базы '${newDbName}' отправлен. Отслеживайте статус.`);
                // Сразу запускаем опрос прогресса для этой базы
                startRestoreProgressPolling(newDbName);
                fetchDatabases(); // Обновляем список баз, чтобы увидеть статус "restoring"
            } else {
                const errorText = await response.text();
                addLogEntry(`ОШИБКА: Не удалось запустить восстановление. Сервер вернул: ${response.status} - ${errorText}`);
                console.error('Ошибка восстановления:', errorText);
                // setRestoreButtonsState('initial'); // Удаляем
                fetchDatabases(); // Обновляем список баз
            }
        } catch (error) {
            addLogEntry(`КРИТИЧЕСКАЯ ОШИБКА: Проблема с сетевым запросом: ${error.message}`);
            console.error('Сетевая ошибка:', error);
            // setRestoreButtonsState('initial'); // Удаляем
            fetchDatabases(); // Обновляем список баз
        }
    }

    const cancelRestore = async (dbName) => { // Принимаем dbName как аргумент
        if (!dbName) {
            alert('Невозможно отменить: имя восстанавливаемой базы не определено.');
            fetchDatabases(); // Обновляем состояние кнопок
            return;
        }

        if (!confirm(`Восстанавливаемая база станет не рабочей и будет удалена. Хотите отменить восстановление базы данных '${dbName}'?`)) {
            addLogEntry('Отказ от отмены. Восстановление продолжается.');
            return;
        }

        addLogEntry(`Отмена восстановления и удаление базы данных '${dbName}'...`);

        try {
            const response = await fetch(`/api/cancel-restore?name=${encodeURIComponent(dbName)}`, {
                method: 'POST',
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }

            const result = await response.json();
            addLogEntry(`УСПЕХ: ${result.message}`);
            // Останавливаем опрос прогресса для этой базы
            if (activeRestorePollers[dbName]) {
                clearInterval(activeRestorePollers[dbName]);
                delete activeRestorePollers[dbName];
            }

        } catch (error) {
            console.error('Ошибка отмены восстановления:', error);
            addLogEntry(`ОШИБКА: Не удалось отменить восстановление: ${error.message}`);
        } finally {
            fetchDatabases(); // Обновляем список баз, чтобы отразить изменения
        }
    };

    // --- Обработчики событий ---

    refreshDbBtn.addEventListener('click', fetchDatabases);
    deleteDbBtn.addEventListener('click', () => deleteDatabase(selectedDatabase)); // Передаем selectedDatabase
    refreshBackupsBtn.addEventListener('click', fetchBackups);

    clearDbNameBtn.addEventListener('click', () => {
        newDbNameInput.value = '';
    });

    setCurrentDatetimeBtn.addEventListener('click', () => {
        const now = new Date();
        restoreDatetimeInput.value = formatDateToInput(now);
        restoreDatetimeInput.focus();
        addLogEntry('Установлено текущее время для восстановления.');
    });

    // Обработчик отправки формы
    restoreForm.addEventListener('submit', async (event) => {
        event.preventDefault(); // Предотвращаем стандартную отправку формы

        const backupBaseName = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();

        if (!backupBaseName || !newDbName) {
            alert('Пожалуйста, выберите бэкап и введите имя восстанавливаемой базы данных.');
            return;
        }

        // Проверяем, существует ли база данных на сервере
        try {
            const response = await fetch('/api/databases');
            const databases = await response.json();
            const dbExists = databases.some(db => db.name.toLowerCase() === newDbName.toLowerCase());

            if (dbExists) {
                // Если существует, переключаемся в режим подтверждения
                addLogEntry(`База данных '${newDbName}' существует. Ожидание подтверждения перезаписи.`);
                // setRestoreButtonsState('confirm'); // Удаляем, управляем видимостью напрямую
                confirmRestoreButtons.style.display = 'flex';
                confirmationSection.style.display = 'block';
                restoreDbBtn.style.display = 'none';
            } else {
                // Если не существует, запускаем восстановление сразу
                startRestoreProcess();
                confirmRestoreButtons.style.display = 'none';
                confirmationSection.style.display = 'none';
                restoreDbBtn.style.display = 'block';
            }
        } catch (error) {
            console.error('Ошибка при проверке существования БД перед восстановлением:', error);
            addLogEntry(`ОШИБКА: Не удалось проверить существование БД: ${error.message}`);
            // setRestoreButtonsState('initial'); // Удаляем
            confirmRestoreButtons.style.display = 'none';
            confirmationSection.style.display = 'none';
            restoreDbBtn.style.display = 'block';
        }
    });

    confirmRestoreBtn.addEventListener('click', () => {
        startRestoreProcess(); // Запускаем без флага confirmOverwrite, так как бэкенд его не использует
        confirmRestoreButtons.style.display = 'none';
        confirmationSection.style.display = 'none';
        restoreDbBtn.style.display = 'block';
    });
    cancelConfirmRestoreBtn.addEventListener('click', () => {
        addLogEntry('Восстановление отменено пользователем (на этапе подтверждения).');
        // setRestoreButtonsState('initial'); // Удаляем
        confirmRestoreButtons.style.display = 'none';
        confirmationSection.style.display = 'none';
        restoreDbBtn.style.display = 'block';
    });

    // Главная кнопка отмены восстановления (для текущей выбранной базы)
    cancelRestoreProcessBtn.addEventListener('click', () => {
        const dbName = cancelRestoreProcessBtn.dataset.dbname || newDbNameInput.value.trim();
        cancelRestore(dbName);
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
            return; // Опрос уже запущен
        }
        addLogEntry(`Запущен опрос прогресса для базы '${dbName}'.`);
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

            if (progress.status === 'completed' || progress.status === 'failed' || progress.status === 'cancelled' || progress.status === 'not_found') {
                addLogEntry(`Восстановление базы '${dbName}' завершено со статусом: ${progress.status}.`);
                if (activeRestorePollers[dbName]) {
                    clearInterval(activeRestorePollers[dbName]);
                    delete activeRestorePollers[dbName];
                }
                fetchDatabases(); // Обновляем список баз, чтобы убрать прогресс-бар и обновить статус
            }

        } catch (error) {
            console.error(`Ошибка получения прогресса для ${dbName}:`, error);
            addLogEntry(`ОШИБКА: Не удалось получить прогресс для базы '${dbName}': ${error.message}`);
            // Если ошибка, останавливаем опрос, чтобы не спамить запросами
            if (activeRestorePollers[dbName]) {
                clearInterval(activeRestorePollers[dbName]);
                delete activeRestorePollers[dbName];
            }
            fetchDatabases(); // Обновляем список баз
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
            progressContainer.style.display = 'flex'; // Показываем прогресс-бар
            progressBarFill.style.width = `${progress.percentage}%`;
            progressText.textContent = `${progress.percentage}%`; // Удалено отображение имени файла
            statusIconSpan.innerHTML = `<i class="fas fa-sync-alt fa-spin restoring" title="Восстанавливается"></i>`;
            statusIconSpan.title = "restoring";
        } else {
            progressContainer.style.display = 'none'; // Скрываем прогресс-бар
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
                    iconClass = 'fas fa-times-circle offline'; // Или другой значок для отмены
                    title = 'Восстановление отменено';
                    break;
                case 'not_found': // Если прогресс не найден, значит, база не восстанавливается
                    // Оставляем текущий статус, который пришел из fetchDatabases
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
    setInterval(fetchDatabases, 10000); // Обновляем список баз каждые 10 секунд
    setInterval(fetchBriefLog, 5000); // Обновляем лог каждые 5 секунд
});

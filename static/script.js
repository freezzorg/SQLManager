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
    const backupEndTimesSelect = document.getElementById('backup-end-times');
    const refreshBackupTimesBtn = document.getElementById('refresh-backup-times-btn');

    // Функция для фильтрации списка дат окончания бэкапов по выбранной дате
    const filterBackupEndTimesByDate = (selectedDateStr) => {
        if (!selectedDateStr) {
            // Если дата не выбрана, показываем все даты
            const options = backupEndTimesSelect.options;
            for (let i = 0; i < options.length; i++) {
                options[i].style.display = 'block';
            }
            return;
        }

        // Преобразуем строку даты в формат YYYY-MM-DD (берем только дату из datetime-local)
        const selectedDate = selectedDateStr.split('T')[0];

        const options = backupEndTimesSelect.options;
        for (let i = 0; i < options.length; i++) {
            const optionValue = options[i].value;
            if (!optionValue) {
                options[i].style.display = 'block'; // Показываем пустые опции
                continue;
            }
            
            // Берем только дату из значения опции
            const optionDate = optionValue.split('T')[0];
            
            // Сравниваем даты (формат YYYY-MM-DD)
            if (optionDate === selectedDate) {
                options[i].style.display = 'block';
            } else {
                options[i].style.display = 'none';
            }
        }
    };

    // Обработчик изменения значения в поле даты/времени восстановления
    restoreDatetimeInput.addEventListener('input', () => {
        const selectedDateStr = restoreDatetimeInput.value;
        filterBackupEndTimesByDate(selectedDateStr);
    });

    const restoreDbBtn = document.getElementById('restore-db-btn');
    const confirmRestoreButtons = document.getElementById('confirm-restore-buttons');
    const confirmRestoreBtn = document.getElementById('confirm-restore-btn');
    const cancelConfirmRestoreBtn = document.getElementById('cancel-confirm-restore-btn');

    const briefLog = document.getElementById('brief-log');
    const restoreForm = document.getElementById('restore-form'); // Добавляем ссылку на форму
    const confirmationSection = document.getElementById('confirmation-section'); // Добавляем ссылку на секцию подтверждения

    let selectedDatabase = null;

    const restoreProgressPollingInterval = 3000; // Интервал опроса прогресса в мс
    const activeRestorePollers = {}; // Хранит setInterval ID для каждой восстанавливаемой БД
    const activeBackupPollers = {}; // Хранит setInterval ID для каждой бэкапируемой БД
    const inProgressRestores = new Set(); // Хранит имена баз, которые находятся в процессе восстановления

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
            const response = await makeApiRequest('/api/log');
            if (response.ok) {
                const logEntries = await response.json();

                briefLog.innerHTML = '';
                logEntries.reverse().forEach(entry => {
                    const li = document.createElement('li');
                    const time = formatDateTime(new Date(entry.timestamp), 'log');
                    li.textContent = `${time} ${entry.message}`;
                    briefLog.appendChild(li);
                });
            } else {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }
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

    // Функция для выполнения API-запросов с обработкой ошибок
    const makeApiRequest = async (url, options = {}) => {
        try {
            const response = await fetch(url, {
                headers: {
                    'Content-Type': 'application/json',
                    ...options.headers
                },
                ...options
            });

            // Возвращаем ответ как есть, чтобы вызывающая сторона могла сама обработать статус
            return response;
        } catch (networkError) {
            // Сетевые ошибки (например, потеря соединения, сервер недоступен) логируются только в консоль
            // Согласно требованиям - сетевые ошибки должны логироваться только в файл, а не пользователю
            console.error('Сетевая ошибка при выполнении запроса:', networkError);
            throw networkError;
        }
    };

    // --- API Функции ---

    const fetchDatabases = async () => {
        try {
            const response = await makeApiRequest('/api/databases');
            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
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
            const response = await makeApiRequest('/api/backups');
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
                addLogEntry(`ОШИБКА: Не удалось обработать дату/время: ${restoreDateTime}. Убедитесь, что формат: ГГГГ-М-ДДТЧЧ:ММ:СС`);
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
            const response = await makeApiRequest('/api/restore', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(requestBody)
            });

            if (response.ok) {
                // Добавляем базу в список восстанавливаемых
                inProgressRestores.add(newDbName);
                
                // Проверяем, существует ли база в списке баз
                const databasesResponse = await makeApiRequest('/api/databases');
                if (databasesResponse.ok) {
                    const databases = await databasesResponse.json();
                    const dbExists = databases.some(db => db.name.toLowerCase() === newDbName.toLowerCase());

                    // Если база не существует в списке, добавляем временный элемент с прогрессбаром
                    if (!dbExists) {
                        // Создаем временный элемент списка базы
                        const li = document.createElement('li');
                        li.dataset.dbname = newDbName;
                        li.className = 'db-item db-state-restoring';
                        
                        const dbNameSpan = document.createElement('span');
                        dbNameSpan.className = 'db-name';
                        dbNameSpan.textContent = newDbName;
                        li.prepend(dbNameSpan);

                        const statusIconSpan = document.createElement('span');
                        statusIconSpan.className = 'db-status-icon';
                        li.insertBefore(statusIconSpan, dbNameSpan.nextSibling);
                        statusIconSpan.innerHTML = '<i class="fas fa-sync-alt fa-spin restoring" title="Восстанавливается"></i>';
                        statusIconSpan.title = "restoring";

                        // Создаем контейнер для прогресса восстановления
                        const restoreProgressContainer = createRestoreProgressContainer(newDbName);
                        restoreProgressContainer.style.display = 'flex';
                        li.appendChild(restoreProgressContainer);

                        // Добавляем элемент в список баз
                        databaseList.appendChild(li);
                    }
                }
                
                startRestoreProgressPolling(newDbName);
                // fetchDatabases(); // Обновляем список баз, чтобы синхронизировать сервером
            } else {
                const errorText = await response.text();
                addLogEntry(`ОШИБКА: Не удалось запустить восстановление базы '${newDbName}'. Сервер вернул: ${response.status} - ${errorText}`);
                console.error('Ошибка восстановления:', errorText);
                fetchDatabases();
            }
        } catch (error) {
            // Сетевые ошибки логируются только в консоль, а не пользователю
            console.error('Сетевая ошибка при восстановлении:', error);
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
            const response = await makeApiRequest(`/api/cancel-restore?name=${encodeURIComponent(dbName)}`, {
                method: 'POST',
            });

            if (response.ok) {
                const result = await response.json();
                addLogEntry(`Отмена восстановления базы '${dbName}' успешно завершена`);
                if (activeRestorePollers[dbName]) {
                    clearInterval(activeRestorePollers[dbName]);
                    delete activeRestorePollers[dbName];
                }
            } else {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
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
            const response = await makeApiRequest('/api/backup', {
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
            // Сетевые ошибки логируются только в консоль, а не пользователю
            console.error('Сетевая ошибка при создании бэкапа:', error);
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

    // Функция для загрузки и отображения дат окончания бэкапов
    const loadBackupEndTimes = async (selectedBackup) => {
        try {
            const response = await makeApiRequest(`/api/backup-metadata?name=${encodeURIComponent(selectedBackup)}`);
            
            if (response.ok) {
                const metadata = await response.json();
                
                // Очищаем текущий список
                backupEndTimesSelect.innerHTML = '';
                
                // Сортируем метаданные по времени окончания в обратном порядке (от более свежих к более ранним)
                metadata.sort((a, b) => new Date(b.End) - new Date(a.End));
                
                // Добавляем даты окончания в список
                metadata.forEach(item => {
                    const option = document.createElement('option');
                    // Форматируем дату для отображения
                    // Создаем объект Date, интерпретируя строку как локальное время
                    const parts = item.End.split('T');
                    const datePart = parts[0].split('-');
                    const timePart = parts[1].split(':');
                    const date = new Date(
                        parseInt(datePart[0]), 
                        parseInt(datePart[1]) - 1, 
                        parseInt(datePart[2]), 
                        parseInt(timePart[0]), 
                        parseInt(timePart[1]), 
                        parseInt(timePart[2])
                    );
                    option.value = formatDateTime(date, 'input');
                    option.textContent = date.toLocaleString('ru-RU');
                    backupEndTimesSelect.appendChild(option);
                });
                
                if (metadata.length === 0) {
                    const option = document.createElement('option');
                    option.value = '';
                    option.textContent = 'Нет доступных дат окончания';
                    backupEndTimesSelect.appendChild(option);
                }
                
                return true;
            } else {
                const errorText = await response.text();
                console.error('Ошибка получения метаданных бэкапа:', errorText);
                addLogEntry(`ОШИБКА: Не удалось получить метаданные бэкапа: ${errorText}`);
                
                // Добавляем сообщение об ошибке
                backupEndTimesSelect.innerHTML = '';
                const option = document.createElement('option');
                option.value = '';
                option.textContent = 'Ошибка загрузки данных';
                backupEndTimesSelect.appendChild(option);
                
                return false;
            }
        } catch (error) {
            console.error('Ошибка при загрузке метаданных бэкапа:', error);
            addLogEntry(`ОШИБКА: Сетевая ошибка при загрузке метаданных бэкапа: ${error.message}`);
            
            // Добавляем сообщение об ошибке
            backupEndTimesSelect.innerHTML = '';
            const option = document.createElement('option');
            option.value = '';
            option.textContent = 'Ошибка загрузки данных';
            backupEndTimesSelect.appendChild(option);
            
            return false;
        }
    };

    // Обработчик выбора бэкапа - загружаем метаданные и формируем список дат окончания
    backupSelect.addEventListener('change', async () => {
        const selectedBackup = backupSelect.value;
        
        if (selectedBackup) {
            await loadBackupEndTimes(selectedBackup);
        } else {
            // Если бэкап не выбран, очищаем список
            backupEndTimesSelect.innerHTML = '<option value="" disabled selected>Выберите бэкап для отображения дат</option>';
        }
    });

    // Обработчик выбора даты окончания бэкапа - устанавливаем значение в поле даты и времени
    backupEndTimesSelect.addEventListener('change', () => {
        const selectedEndTime = backupEndTimesSelect.value;
        
        if (selectedEndTime) {
            // selectedEndTime уже в формате YYYY-MM-DDTHH:mm:ss, подходящем для поля datetime-local
            // которое интерпретируется как локальное время
            restoreDatetimeInput.value = selectedEndTime;
        }
    });

    // Обработчик кнопки обновления списка дат окончания бэкапов
    refreshBackupTimesBtn.addEventListener('click', async () => {
        const selectedBackup = backupSelect.value;
        
        if (selectedBackup) {
            await loadBackupEndTimes(selectedBackup);
        } else {
            alert('Пожалуйста, сначала выберите бэкап.');
        }
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
            const response = await makeApiRequest('/api/databases');
            if (response.ok) {
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
            } else {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
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
            const response = await makeApiRequest(`/api/restore-progress?name=${encodeURIComponent(dbName)}`);
            if (response.ok) {
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
            } else {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }
        } catch (error) {
            console.error(`Ошибка получения прогресса для ${dbName}:`, error);
            // addLogEntry(`ОШИБКА: Не удалось получить прогресс для базы '${dbName}': ${error.message}`);
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
            const response = await makeApiRequest(`/api/backup-progress?name=${encodeURIComponent(dbName)}`);
            if (response.ok) {
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
            } else {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
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

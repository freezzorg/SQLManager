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
    let restoreInProgress = false;

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

    const setRestoreButtonsState = (state) => {
        restoreDbBtn.style.display = 'none';
        confirmRestoreButtons.style.display = 'none';
        cancelRestoreProcessBtn.style.display = 'none';
        confirmationSection.style.display = 'none'; // Скрываем секцию подтверждения по умолчанию

        switch (state) {
            case 'initial':
                restoreDbBtn.style.display = 'block';
                restoreInProgress = false;
                break;
            case 'confirm':
                confirmRestoreButtons.style.display = 'flex';
                confirmationSection.style.display = 'block'; // Показываем секцию подтверждения
                restoreInProgress = false;
                break;
            case 'in_progress':
                cancelRestoreProcessBtn.style.display = 'block';
                restoreInProgress = true;
                break;
        }
        fetchDatabases();
    };

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

            databaseList.innerHTML = '';
            databases.forEach(db => {
                const li = document.createElement('li');
                li.className = `db-item db-state-${db.state}`;
                li.textContent = `${db.name} (${db.state})`;

                li.addEventListener('click', () => {
                    selectedDatabase = db.name;
                    newDbNameInput.value = db.name;

                    document.querySelectorAll('.db-item').forEach(item => {
                        item.classList.remove('selected');
                    });
                    li.classList.add('selected');
                });

                databaseList.appendChild(li);
            });

            if (databases.some(db => db.state === 'restoring')) {
                setRestoreButtonsState('in_progress');
            } else if (restoreInProgress) {
                setRestoreButtonsState('initial');
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

        if (restoreInProgress) {
            addLogEntry("Предупреждение: Процесс восстановления уже запущен.");
            return;
        }

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
        setRestoreButtonsState('in_progress');

        const requestBody = {
            backupBaseName: backupBaseName,
            newDbName: newDbName,
            restoreDateTime: formattedDateTime,
            confirmOverwrite: confirmOverwrite // Добавляем флаг подтверждения
        };

        try {
            restoreInProgress = true;
            const response = await fetch('/api/restore', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(requestBody)
            });

            if (response.ok) {
                addLogEntry(`УСПЕХ: Запрос на восстановление базы '${newDbName}' отправлен. Отслеживайте статус.`);
            } else {
                const errorText = await response.text();
                addLogEntry(`ОШИБКА: Не удалось запустить восстановление. Сервер вернул: ${response.status} - ${errorText}`);
                console.error('Ошибка восстановления:', errorText);
                setRestoreButtonsState('initial');
            }
        } catch (error) {
            addLogEntry(`КРИТИЧЕСКАЯ ОШИБКА: Проблема с сетевым запросом: ${error.message}`);
            console.error('Сетевая ошибка:', error);
            setRestoreButtonsState('initial');
        }
    }

    const cancelRestore = async () => {
        const dbName = newDbNameInput.value.trim();
        if (!dbName) {
            alert('Невозможно отменить: имя восстанавливаемой базы не определено.');
            setRestoreButtonsState('initial');
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

        } catch (error) {
            console.error('Ошибка отмены восстановления:', error);
            addLogEntry(`ОШИБКА: Не удалось отменить восстановление: ${error.message}`);
        } finally {
            setRestoreButtonsState('initial');
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
                setRestoreButtonsState('confirm');
            } else {
                // Если не существует, запускаем восстановление сразу
                startRestoreProcess();
            }
        } catch (error) {
            console.error('Ошибка при проверке существования БД перед восстановлением:', error);
            addLogEntry(`ОШИБКА: Не удалось проверить существование БД: ${error.message}`);
            setRestoreButtonsState('initial');
        }
    });

    confirmRestoreBtn.addEventListener('click', () => startRestoreProcess(true)); // Передаем true для подтверждения
    cancelConfirmRestoreBtn.addEventListener('click', () => {
        addLogEntry('Восстановление отменено пользователем (на этапе подтверждения).');
        setRestoreButtonsState('initial');
    });

    cancelRestoreProcessBtn.addEventListener('click', cancelRestore);

    // --- Инициализация ---
    fetchDatabases();
    fetchBackups();
    fetchBriefLog();
    setInterval(fetchDatabases, 10000);
    setInterval(fetchBriefLog, 5000);
});

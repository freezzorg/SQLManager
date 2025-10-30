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

    let selectedDatabase = null; 
    let restoreInProgress = false; 

    // --- Утилиты ---
    
    const addLogEntry = (message) => {
        // Мы полагаемся на fetchBriefLog для реального лога, 
        // но можем добавить немедленное сообщение для UX
        console.log(message);
    };

    const fetchBriefLog = async () => {
        try {
            const response = await fetch('/api/log');
            if (!response.ok) throw new Error('Ошибка получения лога');
            const logEntries = await response.json();
            
            briefLog.innerHTML = '';
            logEntries.reverse().forEach(entry => {
                const p = document.createElement('p');
                const time = new Date(entry.timestamp).toLocaleTimeString('ru-RU');
                p.textContent = `[${time}] ${entry.message}`;
                briefLog.appendChild(p);
            });
        } catch (error) {
            console.error('Ошибка получения краткого лога:', error);
        }
    };
    
    // Функция для перехода по состояниям кнопок
    const setRestoreButtonsState = (state) => {
        // Скрываем все
        restoreDbBtn.style.display = 'none';
        confirmRestoreButtons.style.display = 'none';
        cancelRestoreProcessBtn.style.display = 'none';

        switch (state) {
            case 'initial':
                restoreDbBtn.style.display = 'block'; // Кнопка "Восстановить базу данных"
                restoreInProgress = false;
                break;
            case 'confirm':
                confirmRestoreButtons.style.display = 'flex'; // Кнопки "Восстановить" и "Отменить"
                restoreInProgress = false;
                break;
            case 'in_progress':
                cancelRestoreProcessBtn.style.display = 'block'; // Кнопка "Отменить восстановление"
                restoreInProgress = true;
                break;
        }
        fetchDatabases(); // Обновляем список баз, чтобы увидеть состояние RESTORING
    };

    // Форматирует объект Date в строку YYYY-MM-DD HH:MM:SS
    function formatDateToBackend(date) {
        const y = date.getFullYear();
        const m = String(date.getMonth() + 1).padStart(2, '0');
        const d = String(date.getDate()).padStart(2, '0');
        const h = String(date.getHours()).padStart(2, '0');
        const min = String(date.getMinutes()).padStart(2, '0');
        const s = String(date.getSeconds()).padStart(2, '0');
        // Формат: YYYY-MM-DD HH:MM:SS
        return `${y}-${m}-${d} ${h}:${min}:${s}`;
    }

    // --- API Функции ---

    const fetchDatabases = async () => {
        // ... (логика получения и отображения списка баз данных)
        try {
            const response = await fetch('/api/databases');
            const databases = await response.json();

            databaseList.innerHTML = ''; // Очистка списка
            databases.forEach(db => {
                const li = document.createElement('li');
                li.className = `db-item db-state-${db.state}`;
                li.textContent = `${db.name} (${db.state})`;
                
                // Обработчик для выбора базы и копирования имени
                li.addEventListener('click', () => {
                    selectedDatabase = db.name;
                    newDbNameInput.value = db.name; 
                    
                    // Убираем подсветку со всех элементов
                    document.querySelectorAll('.db-item').forEach(item => {
                        item.classList.remove('selected');
                    });
                    // Подсвечиваем выбранный
                    li.classList.add('selected');
                });
                
                databaseList.appendChild(li);
            });
            
            // Если база находится в состоянии restoring, переводим кнопки в in_progress
            if (databases.some(db => db.state === 'restoring')) {
                setRestoreButtonsState('in_progress');
            } else if (restoreInProgress) {
                // Если флаг был true, но базы нет в restoring, значит восстановление завершено/остановлено
                setRestoreButtonsState('initial');
            }

        } catch (error) {
            console.error('Ошибка получения списка баз данных:', error);
            addLogEntry(`ОШИБКА: Не удалось получить список баз данных: ${error.message}`);
        }
    };

    const fetchBackups = async () => {
        // ... (логика получения и отображения списка бэкапов)
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
                option.value = backup.baseName; // Имя директории
                option.textContent = backup.baseName;
                backupSelect.appendChild(option);
            });
        } catch (error) {
            console.error('Ошибка получения списка бэкапов:', error);
            addLogEntry(`ОШИБКА: Не удалось получить список бэкапов: ${error.message}`);
        }
    };
    
    // Функция удаления базы данных
    const deleteDatabase = async () => {
        if (!selectedDatabase) {
            alert('Пожалуйста, выберите базу данных для удаления.');
            return;
        }
        if (!confirm(`Вы действительно хотите удалить базу данных '${selectedDatabase}'?`)) {
            return;
        }

        try {
            const response = await fetch(`/api/delete?name=${encodeURIComponent(selectedDatabase)}`, {
                method: 'DELETE',
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Ошибка сервера: ${response.status} - ${errorText}`);
            }

            const result = await response.json();
            addLogEntry(`УСПЕХ: ${result.message}`);
            selectedDatabase = null; // Сброс выбранной базы
            fetchDatabases();
        } catch (error) {
            console.error('Ошибка удаления базы данных:', error);
            addLogEntry(`ОШИБКА: Не удалось удалить базу данных: ${error.message}`);
        }
    };


    // Функция запуска восстановления
    async function startRestoreProcess() {
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
        
        // --- ИСПРАВЛЕНИЕ ЛОГИКИ ПАРСИНГА ДАТЫ/ВРЕМЕНИ ---
        if (restoreDateTime !== "") {
            try {
                // 1. Обработка формата YYYY-MM-DDTHH:MM (от datetime-local, без секунд)
                if (restoreDateTime.includes('T')) {
                    // Заменяем 'T' на пробел и добавляем секунды. 
                    // Пример: '2025-10-29T15:03' -> '2025-10-29 15:03:00'
                    formattedDateTime = restoreDateTime.replace('T', ' ') + ':00';
                } 
                // 2. Обработка формата YYYY-MM-DD HH:MM (введен вручную, без секунд)
                else if (restoreDateTime.split(' ').length === 2 && restoreDateTime.split(':').length === 2 && restoreDateTime.split(':').length === 2) {
                    // Если формат 'YYYY-MM-DD HH:MM' (введен вручную без секунд)
                    formattedDateTime = restoreDateTime + ':00';
                } 
                // 3. Предполагаем, что это уже корректный формат YYYY-MM-DD HH:MM:SS (включая нажатие "Сейчас")
                else {
                    formattedDateTime = restoreDateTime;
                }

                if (!formattedDateTime.match(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)) {
                    throw new Error("Не соответствует формату ГГГГ-ММ-ДД ЧЧ:ММ:СС");
                }
                
                addLogEntry(`Восстановление на момент времени (PIRT) будет выполнено до: ${formattedDateTime}`);

            } catch (e) {
                console.error("Ошибка форматирования даты:", e);
                addLogEntry(`ОШИБКА: Не удалось обработать дату/время: ${restoreDateTime}. Убедитесь, что формат: ГГГГ-ММ-ДД ЧЧ:ММ:СС`);
                return;
            }
        } 
        // Если restoreDateTime пуст, formattedDateTime остается "", и бэкенд восстановит до последнего бэкапа.
        
        // --- Отправка запроса ---
        addLogEntry(`Запуск восстановления базы данных '${newDbName}' из бэкапа '${backupBaseName}'...`);
        setRestoreButtonsState('restoring'); // Устанавливаем кнопку "Отменить восстановление"

        const requestBody = {
            backupBaseName: backupBaseName,
            newDbName: newDbName,
            restoreDateTime: formattedDateTime // Отправляем отформатированную строку (или "" если не задана)
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
                setRestoreButtonsState('initial'); // Возвращаемся в исходное состояние при ошибке
            }
        } catch (error) {
            addLogEntry(`КРИТИЧЕСКАЯ ОШИБКА: Проблема с сетевым запросом: ${error.message}`);
            console.error('Сетевая ошибка:', error);
            setRestoreButtonsState('initial'); 
        }
    }

    // Функция отмены восстановления
    const cancelRestore = async () => {
        const dbName = newDbNameInput.value.trim();
        if (!dbName) {
            alert('Невозможно отменить: имя восстанавливаемой базы не определено.');
            setRestoreButtonsState('initial');
            return;
        }

        // Вывод окна с предупреждением
        if (!confirm(`Восстанавливаемая база станет не рабочей и будет удалена. Хотите отменить восстановление базы данных '${dbName}'?`)) {
            addLogEntry('Отказ от отмены. Восстановление продолжается.');
            return; // Отказ от отмены
        }

        addLogEntry(`Отмена восстановления и удаление базы данных '${dbName}'...`);
        
        try {
            // ИСПРАВЛЕНО: API-маршрут /api/cancel-restore и метод POST
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
            setRestoreButtonsState('initial'); // Всегда возвращаемся в исходное состояние
        }
    };


    // --- Обработчики событий ---

    refreshDbBtn.addEventListener('click', fetchDatabases);
    deleteDbBtn.addEventListener('click', deleteDatabase);
    refreshBackupsBtn.addEventListener('click', fetchBackups);
    
    // Кнопка "Очистить" имя базы
    clearDbNameBtn.addEventListener('click', () => {
        newDbNameInput.value = '';
    });

    // Кнопка "Сейчас"
    setCurrentDatetimeBtn.addEventListener('click', () => {
        const now = new Date();
        restoreDatetimeInput.value = formatDateToBackend(now);
        restoreDatetimeInput.focus(); // Для лучшего UX
        addLogEntry('Установлено текущее время для восстановления.');
    });

    // Основная кнопка "Восстановить базу данных"
    restoreDbBtn.addEventListener('click', async () => {
        const selectedBackup = backupSelect.value;
        const newDbName = newDbNameInput.value.trim();
        if (!selectedBackup) {
            alert('Пожалуйста, выберите бэкап.');
            return;
        }
        if (!newDbName) {
            alert('Пожалуйста, введите имя восстанавливаемой базы данных.');
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

    // Кнопки режима подтверждения
    confirmRestoreBtn.addEventListener('click', startRestoreProcess); // "Восстановить"
    cancelConfirmRestoreBtn.addEventListener('click', () => {
        addLogEntry('Восстановление отменено пользователем (на этапе подтверждения).');
        setRestoreButtonsState('initial'); // "Отменить"
    });
    
    // Кнопка отмены во время процесса
    cancelRestoreProcessBtn.addEventListener('click', cancelRestore);

    // --- Инициализация ---\
    fetchDatabases();
    fetchBackups();
    fetchBriefLog();
    setInterval(fetchDatabases, 10000); // Обновляем список БД
    setInterval(fetchBriefLog, 5000); // Обновляем лог каждые 5 секунд
});
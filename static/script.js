document.addEventListener('DOMContentLoaded', () => {
    const databaseList = document.getElementById('database-list');
    const deleteDbBtn = document.getElementById('delete-db-btn');
    const backupDbBtn = document.getElementById('backup-db-btn');
    const refreshDbBtn = document.getElementById('refresh-db-btn');
    
    const backupSelect = document.getElementById('backup-select');
    const refreshBackupsBtn = document.getElementById('refresh-backups-btn');
    const newDbNameInput = document.getElementById('new-db-name');
    const clearDbNameBtn = document.getElementById('clear-db-name-btn');
    const restoreDatetimeInput = document.getElementById('restore-datetime');
    const datetimePickerBtn = document.getElementById('datetime-picker-btn');
    const setCurrentDatetimeBtn = document.getElementById('set-current-datetime-btn');
    const backupEndTimesSelect = document.getElementById('backup-end-times');
    const refreshBackupTimesBtn = document.getElementById('refresh-backup-times-btn');

    const mask = '__.__.____ __:__:__';
    const editablePositions = [];
    for (let i = 0; i < mask.length; i++) {
        if (mask[i] === '_') editablePositions.push(i);
    }
    const originalInputValue = restoreDatetimeInput.value;
    restoreDatetimeInput.value = "";

    function setCaretPosition(elem, pos) {
        window.requestAnimationFrame(() => elem.setSelectionRange(pos, pos));
    }

    function isEditablePosition(pos) {
        return editablePositions.includes(pos);
    }

    function findNextEditablePosition(pos) {
        for (let i = 0; i < editablePositions.length; i++) {
            if (editablePositions[i] > pos) return editablePositions[i];
        }
        return editablePositions[editablePositions.length - 1] + 1;
    }

    function findPrevEditablePosition(pos) {
        for (let i = editablePositions.length - 1; i >= 0; i--) {
            if (editablePositions[i] < pos) return editablePositions[i];
        }
        return -1;
    }

    // Проверка корректности вводимого символа по позиции
    function validateCharAtPos(valueArr, pos, char) {
        // Копируем массив и вставляем символ
        let testArr = valueArr.slice();
        testArr[pos] = char;

        function isDigit(c) {
            return /\d/.test(c);
        }

        // Проверка по позициям:
        // день: pos 0 и 1
        if (pos === 0) {
            // Первая цифра дня: 0..3
            if (char < '0' || char > '3') return false;
        } else if (pos === 1) {
            // Вторая цифра дня зависит от первой
            const first = testArr[0];
            if (!isDigit(first)) return false;
            if (first === '3') {
                if (char < '0' || char > '1') return false;
            } else {
                if (char < '0' || char > '9') return false;
            }
        }

        // месяц: pos 3 и 4
        if (pos === 3) {
            // Первая цифра месяца: 0..1
            if (char < '0' || char > '1') return false;
        } else if (pos === 4) {
            // Вторая цифра месяца зависит от первой
            const first = testArr[3];
            if (!isDigit(first)) return false;
            if (first === '1') {
                if (char < '0' || char > '2') return false;
            } else {
                if (char < '0' || char > '9') return false;
            }
        }

        // год: pos 6,7,8,9 - позволим вводить 2000-2099 строго на 6 и 7 позиции
        if (pos === 6) {
            if (char !== '2') return false; // первый символ года всегда 2
        }
        if (pos === 7) {
            if (char !== '0') return false; // второй символ года всегда 0
        }
        if (pos === 8) {
            if (char < '0' || char > '9') return false;
        }
        if (pos === 9) {
            if (char < '0' || char > '9') return false;
        }

        // часы: pos 11,12
        if (pos === 11) {
            if (char < '0' || char > '2') return false;
        } else if (pos === 12) {
            const first = testArr[11];
            if (!isDigit(first)) return false;
            if (first === '2') {
                if (char < '0' || char > '3') return false;
            } else {
                if (char < '0' || char > '9') return false;
            }
        }

        // минуты: pos 14,15
        if (pos === 14) {
            if (char < '0' || char > '5') return false;
        } else if (pos === 15) {
            if (char < '0' || char > '9') return false;
        }

        // секунды: pos 17,18
        if (pos === 17) {
            if (char < '0' || char > '5') return false;
        } else if (pos === 18) {
            if (char < '0' || char > '9') return false;
        }

        return true;
    }

    const fp = flatpickr(restoreDatetimeInput, {
        enableTime: false,
        enableSeconds: false,
        dateFormat: "d.m.Y H:i:S",
        time_24hr: true,
        allowInput: true,
        clickOpens: false,
        defaultDate: "",
        locale: "ru",
        onChange: function(selectedDates, dateStr, instance) {
            if (selectedDates.length) {
                // dateStr уже будет в формате "дд.мм.гггг чч:мм:сс"
                restoreDatetimeInput.value = dateStr;
                restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true}));
            } else {
                // Если дата не выбрана, восстановить маску
                if (!restoreDatetimeInput.value || restoreDatetimeInput.value.replace(/_/g, '').trim().length === 0) {
                    restoreDatetimeInput.value = mask;
                }
            }
        },
        onReady: function(selectedDates, dateStr, instance) {
            if (restoreDatetimeInput.value === "") {
                restoreDatetimeInput.value = originalInputValue;
            }
            // Добавляем прокрутку колёсиком мыши для смены месяца
            instance.calendarContainer.addEventListener("wheel", function(e) {
                e.preventDefault();
                if (e.deltaY < 0) {
                    instance.changeMonth(-1); // прокрутка вверх — предыдущий месяц
                } else {
                    instance.changeMonth(1);  // прокрутка вниз — следующий месяц
                }
            });
        }
    });

    // Ограничения для ввода
    const MIN_YEAR = 2000, MAX_YEAR = 2099;

    function isLeapYear(year) {
        return (year % 4 === 0 && year % 100 !== 0) || (year % 400 === 0);
    }
    
    function maxDaysInMonth(year, month) {
        if (month < 1 || month > 12) return 31;
        const days = [31, (isLeapYear(year)?29:28),31,30,31,30,31,31,30,31,30,31];
        return days[month-1];
    }

    // МАСКА с ограничениями!
    function formatAndValidate(value, setBorder=true) {
        // Оставляем только цифры
        const digits = value.replace(/\D/g, '');

        let day = '', month = '', year = '', hour = '', minute = '', second = '';
        let result = '';
        let valid = true;

        // day
        if (digits.length > 0) {
            day = digits.substring(0, 2);
            // Ограничение на первую цифру дня
            if (day.length === 1) {
                if (day < '0' || day > '3') valid = false;
            } else if (day.length === 2) {
                let d = Number(day);
                if (d < 1 || d > 31) valid = false;
            }
            result += day;
        }
        // month
        if (digits.length >= 3) {
            month = digits.substring(2, 4);
            if (month.length === 1) {
                if (month < '0' || month > '1') valid = false;
            } else if (month.length === 2) {
                let m = Number(month);
                if (m < 1 || m > 12) valid = false;
            }
            result += '.' + month;
        }
        // year
        if (digits.length >= 5) {
            year = digits.substring(4, 8);
            if (year.length > 0 && (Number(year)<MIN_YEAR || Number(year)>MAX_YEAR)) valid = false;
            result += '.' + year;
        }
        // hour
        if (digits.length >= 9) {
            hour = digits.substring(8, 10);
            if (hour.length === 2 && (Number(hour)<0 || Number(hour)>23)) valid = false;
            result += ' ' + hour;
        }
        // minute
        if (digits.length >= 11) {
            minute = digits.substring(10, 12);
            if (minute.length === 2 && (Number(minute)<0 || Number(minute)>59)) valid = false;
            result += ':' + minute;
        }
        // second
        if (digits.length >= 13) {
            second = digits.substring(12, 14);
            if (second.length === 2 && (Number(second)<0 || Number(second)>59)) valid = false;
            result += ':' + second;
        }

        // Проверка на месяц/день после полного ввода
        if (day.length === 2 && month.length === 2 && year.length === 4) {
            if (valid) {
                let d = Number(day), m = Number(month), y = Number(year);
                if (d < 1 || d > maxDaysInMonth(y, m)) valid = false;
            }
        }

        // Подсветка ошибки
        if (setBorder) restoreDatetimeInput.style.borderColor = (valid||digits.length===0) ? '' : 'red';

        return {formatted: result, digits, valid};
    }

    // Инициализация поля ввода маской
    restoreDatetimeInput.value = mask;

    // Фильтрация по дню (одна цифра)
    function filterBackupEndTimesByDayPrefix(dayPrefix) {
        const options = backupEndTimesSelect.options;
        if (!dayPrefix) {
            for (let i = 0; i < options.length; i++) options[i].style.display = 'block';
            return;
        }
        for (let i = 0; i < options.length; i++) {
            const val = options[i].value;
            if (!val) {
                options[i].style.display = 'block';
                continue;
            }
            // Дата из значения
            const optionDate = val.split('T')[0]; // format YYYY-MM-DD
            // сравнение по дню
            const optDay = optionDate.split('-')[2];
            if (optDay.startsWith(dayPrefix)) {
                options[i].style.display = 'block';
            } else {
                options[i].style.display = 'none';
            }
        }
    }

    // Основная фильтрация restore-datetime
    restoreDatetimeInput.addEventListener('input', function(e) {
        // Здесь мы не форматируем, так как маска уже делает это
        // const {formatted, digits, valid} = formatAndValidate(e.target.value, true);
        // e.target.value = formatted;

        // Для фильтрации нам нужны только цифры
        const digits = e.target.value.replace(/\D/g, '');

        // ФИЛЬТРАЦИЯ:
        if (digits.length === 1) {
            // Фильтрация по первой цифре дня (например, 1 — 10..19)
            filterBackupEndTimesByDayPrefix(digits);
        } else if (digits.length >= 2) {
            // Фильтр по полному дню
            const now = new Date();
            let month = now.getMonth()+1;
            let year = now.getFullYear();
            if (digits.length >= 4)  month = digits.substring(2,4);
            if (digits.length >= 8)  year = digits.substring(4,8);
            const day = digits.substring(0,2).padStart(2,'0');
            const monthStr = String(month).padStart(2,'0');
            const yearStr  = String(year);
            const dateISO = `${yearStr}-${monthStr}-${day}`;
            // Сравниваем по дате
            const options = backupEndTimesSelect.options;
            for (let i = 0; i < options.length; i++) {
                const val = options[i].value;
                if (!val) {
                    options[i].style.display = 'block';
                    continue;
                }
                const optionDate = val.split('T')[0];
                if (optionDate == dateISO) {
                    options[i].style.display = 'block';
                } else {
                    options[i].style.display = 'none';
                }
            }
        } else {
            // Нет дня — показывать все
            filterBackupEndTimesByDayPrefix('');
        }
    });

    restoreDatetimeInput.addEventListener('focus', () => {
        if (!restoreDatetimeInput.value || restoreDatetimeInput.value === '') restoreDatetimeInput.value = mask;
        let pos = restoreDatetimeInput.value.indexOf('_');
        if (pos === -1) pos = mask.length;
        setCaretPosition(restoreDatetimeInput, pos);
    });

    restoreDatetimeInput.addEventListener('keydown', (e) => {
        const pos = restoreDatetimeInput.selectionStart;

        if (e.key === 'Backspace') {
            e.preventDefault();

            let deletePos = pos - 1;
            while (deletePos >= 0 && !isEditablePosition(deletePos)) {
                deletePos--;
            }
            if (deletePos < 0) return;

            let valueArr = restoreDatetimeInput.value.split('');
            valueArr[deletePos] = '_';
            restoreDatetimeInput.value = valueArr.join('');
            setCaretPosition(restoreDatetimeInput, deletePos);
            restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
        } else if (e.key === 'Delete') {
            e.preventDefault();

            let deletePos = pos;
            while (deletePos < mask.length && !isEditablePosition(deletePos)) {
                deletePos++;
            }
            if (deletePos >= mask.length) return;

            let valueArr = restoreDatetimeInput.value.split('');
            valueArr[deletePos] = '_';
            restoreDatetimeInput.value = valueArr.join('');
            
            let newCaretPos = pos;
            // Проверяем, находится ли следующая позиция после удаленного символа разделителем
            if (deletePos + 1 < mask.length && !isEditablePosition(deletePos + 1)) {
                newCaretPos = pos + 2; // Перемещаем курсор за разделитель
            } else {
                newCaretPos = pos + 1; // Обычное смещение на 1
            }
            setCaretPosition(restoreDatetimeInput, newCaretPos);
            restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
        } else if (e.key.length === 1 && /\d/.test(e.key)) {
            e.preventDefault();

            let insertPos = pos;
            if (!isEditablePosition(insertPos)) {
                insertPos = findNextEditablePosition(insertPos);
            }
            if (insertPos >= mask.length) return;

            let valueArr = restoreDatetimeInput.value.split('');
            if (!validateCharAtPos(valueArr, insertPos, e.key)) {
                // Невалидный ввод, пропускаем
                return;
            }

            valueArr[insertPos] = e.key;
            restoreDatetimeInput.value = valueArr.join('');

            let nextPos = findNextEditablePosition(insertPos);
            if (nextPos > mask.length) nextPos = mask.length;
            setCaretPosition(restoreDatetimeInput, nextPos);
            restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
        } else if (e.key === 'ArrowLeft') {
            e.preventDefault();
            let newPos = pos - 1;
            while (newPos >= 0 && !isEditablePosition(newPos)) {
                newPos--;
            }
            if (newPos < 0) newPos = findNextEditablePosition(-1); // Переход к первой редактируемой позиции
            setCaretPosition(restoreDatetimeInput, newPos);
        } else if (e.key === 'ArrowRight') {
            e.preventDefault();
            let newPos = pos + 1;
            while (newPos < mask.length && !isEditablePosition(newPos)) {
                newPos++;
            }
            if (newPos >= mask.length) newPos = findPrevEditablePosition(mask.length); // Переход к последней редактируемой позиции
            setCaretPosition(restoreDatetimeInput, newPos);
        } else if (
            e.key === 'Tab' || e.key === 'Home' || e.key === 'End' ||
            (e.ctrlKey || e.metaKey) // Разрешаем Ctrl/Cmd + A, C, V, X
        ) {
            return;
        } else {
            e.preventDefault();
        }
    });

    restoreDatetimeInput.addEventListener('click', () => {
        let pos = restoreDatetimeInput.selectionStart;
        if (!isEditablePosition(pos)) {
            let next = findNextEditablePosition(pos);
            if (next >= mask.length) next = pos;
            setCaretPosition(restoreDatetimeInput, next);
        }
    });

    restoreDatetimeInput.addEventListener('blur', () => {
        if (!restoreDatetimeInput.value || restoreDatetimeInput.value.replace(/_/g, '').trim().length === 0) {
            restoreDatetimeInput.value = mask;
        }
        restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
    });

    // Обработчик paste для restoreDatetimeInput
    restoreDatetimeInput.addEventListener('paste', (e) => {
        e.preventDefault();
        const text = (e.clipboardData || window.clipboardData).getData('text');
        const digits = text.replace(/\D/g, '');
        
        let valueArr = mask.split('');
        let digitIndex = 0;
        for (let i = 0; i < editablePositions.length; i++) {
            const pos = editablePositions[i];
            if (digitIndex < digits.length) {
                const char = digits[digitIndex];
                if (validateCharAtPos(valueArr, pos, char)) {
                    valueArr[pos] = char;
                    digitIndex++;
                } else {
                    // Если символ невалиден, прекращаем вставку
                    break;
                }
            } else {
                break;
            }
        }
        restoreDatetimeInput.value = valueArr.join('');
        restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
        
        // Устанавливаем каретку на следующую редактируемую позицию
        let nextPos = findNextEditablePosition(editablePositions[digitIndex - 1] || -1);
        if (nextPos > mask.length) nextPos = mask.length;
        setCaretPosition(restoreDatetimeInput, nextPos);
    });

    function setCurrentRestoreDateTimeToNow() {
        const now = new Date();
        const d = String(now.getDate()).padStart(2, '0');
        const m = String(now.getMonth() + 1).padStart(2, '0');
        const y = now.getFullYear();
        const h = String(now.getHours()).padStart(2, '0');
        const min = String(now.getMinutes()).padStart(2, '0');
        const s = String(now.getSeconds()).padStart(2, '0');
        
        const formattedDateTime = `${d}.${m}.${y} ${h}:${min}:${s}`;
        restoreDatetimeInput.value = formattedDateTime;
        restoreDatetimeInput.style.borderColor = '';
        restoreDatetimeInput.focus();
        restoreDatetimeInput.dispatchEvent(new Event('input', {bubbles:true})); // Для обновления фильтрации
    }
    
    if (setCurrentDatetimeBtn) setCurrentDatetimeBtn.onclick = setCurrentRestoreDateTimeToNow;

    const restoreDbBtn = document.getElementById('restore-db-btn');
    const confirmRestoreButtons = document.getElementById('confirm-restore-buttons');
    const confirmRestoreBtn = document.getElementById('confirm-restore-btn');
    const cancelConfirmRestoreBtn = document.getElementById('cancel-confirm-restore-btn');

    const briefLog = document.getElementById('brief-log');
    const restoreForm = document.getElementById('restore-form');
    const confirmationSection = document.getElementById('confirmation-section');

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

    // Функция для парсинга даты из формата маски (Д.ММ.ГГГГ ЧЧ:ММ[:СС]) в ISO формат
    function parseAndValidateMaskedDateTime(maskedDateTime) {
        // Проверяем формат ДД.ММ.ГГГГ ЧЧ:М:СС или ДД.ММ.ГГГГ ЧЧ:ММ
        const regex = /^(\d{2})\.(\d{2})\.(\d{4})\s(\d{2}):(\d{2})(?::(\d{2}))?$/;
        const match = maskedDateTime.match(regex);
        
        if (!match) {
            return null;
        }
        
        const [, day, month, year, hour, minute, seconds] = match;
        
        // Проверяем валидность значений
        const d = parseInt(day, 10);
        const m = parseInt(month, 10);
        const y = parseInt(year, 10);
        const h = parseInt(hour, 10);
        const min = parseInt(minute, 10);
        const sec = seconds ? parseInt(seconds, 10) : 0;
        
        if (d < 1 || d > 31 || m < 1 || m > 12 || h < 0 || h > 23 || min < 0 || min > 59 || sec < 0 || sec > 59) {
            return null;
        }
        
        // Возвращаем дату в формате ISO (YYYY-MM-DD) и время (HH:MM:SS)
        return {
            dateISO: `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`,
            time: `${String(h).padStart(2, '0')}:${String(min).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
        };
    }

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
                // Преобразуем дату из формата маски в ISO строку для парсинга
                const parsed = parseAndValidateMaskedDateTime(restoreDateTime);
                if (!parsed) {
                    throw new Error("Некорректный формат даты/времени");
                }
                
                // Создаем объект Date из ISO строки
                const dateObj = new Date(parsed.dateISO + 'T' + parsed.time);
                if (isNaN(dateObj.getTime())) {
                    throw new Error("Некорректный формат даты/времени");
                }
                
                formattedDateTime = formatDateTime(dateObj, 'backend');

            } catch (e) {
                console.error("Ошибка форматирования даты:", e);
                addLogEntry(`ОШИБКА: Не удалось обработать дату/время: ${restoreDateTime}. Убедитесь, что формат: ГГГГ-М-ДДТЧ:ММ:СС`);
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

    // При клике по кнопке календаря — открываем скрытый input
    datetimePickerBtn.addEventListener('click', function() {
        if (fp) {
            fp.open();
        }
    });

    // Обработчик кнопки "Сейчас" - устанавливает текущую дату и время
    setCurrentDatetimeBtn.addEventListener('click', () => {
        const now = new Date();
        // Преобразуем дату в формат, соответствующий маске ввода (ДД.ММ.ГГГГ ЧЧ:М)
        const day = String(now.getDate()).padStart(2, '0');
        const month = String(now.getMonth() + 1).padStart(2, '0');
        const year = now.getFullYear();
        const hours = String(now.getHours()).padStart(2, '0');
        const minutes = String(now.getMinutes()).padStart(2, '0');
        const seconds = String(now.getSeconds()).padStart(2, '0');
        
        const formattedDateTime = `${day}.${month}.${year} ${hours}:${minutes}:${seconds}`;
        restoreDatetimeInput.value = formattedDateTime;
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
                    
                    // Форматируем дату вручную в формате ДД.ММ.ГГГ ЧЧ:мм:сс
                    const day = String(date.getDate()).padStart(2, '0');
                    const month = String(date.getMonth() + 1).padStart(2, '0');
                    const year = date.getFullYear();
                    const hours = String(date.getHours()).padStart(2, '0');
                    const minutes = String(date.getMinutes()).padStart(2, '0');
                    const seconds = String(date.getSeconds()).padStart(2, '0');
                    
                    // Определяем тип бэкапа для отображения
                    let backupType, backupClass;
                    switch(item.Type) {
                        case "Database":
                            backupType = "Полный бэкап";
                            break;
                        case "Database Differential":
                            backupType = "Дифференциальный бэкап";
                            break;
                        case "Transaction Log":
                            backupType = "Бэкап журналов транзакций";
                            break;
                        default:
                            backupType = item.Type;
                    }
                    
                    option.value = formatDateTime(date, 'input');
                    option.textContent = `${day}.${month}.${year} ${hours}:${minutes}:${seconds}  - ${backupType}`;
                    option.className = backupClass;
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
            // Ожидаемый формат: YYYY-MM-DDTHH:MM:SS
            const [date, time] = selectedEndTime.split('T');
            const [yyyy, mm, dd] = date.split('-');
            const [hh, min, sec] = time.split(':');
            restoreDatetimeInput.value = `${dd}.${mm}.${yyyy} ${hh}:${min}:${sec}`;
            restoreDatetimeInput.style.borderColor = '';
            for (let i = 0; i < backupEndTimesSelect.options.length; i++) {
                if (backupEndTimesSelect.options[i].value === selectedEndTime) {
                    backupEndTimesSelect.options[i].style.display = 'block';
                } else {
                    backupEndTimesSelect.options[i].style.display = 'none';
                }
            }
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

// script.js

function setCurrentDateTime() {
    var now = new Date();
    var year = now.getFullYear();
    var month = String(now.getMonth() + 1).padStart(2, '0');
    var day = String(now.getDate()).padStart(2, '0');
    var hours = String(now.getHours()).padStart(2, '0');
    var minutes = String(now.getMinutes()).padStart(2, '0');
    var datetimeValue = year + '-' + month + '-' + day + 'T' + hours + ':' + minutes;
    document.getElementById('restoreTime').value = datetimeValue;
}

function clearNewDb() {
    document.getElementById('newDb').value = '';
}

document.addEventListener('DOMContentLoaded', function() {
    let selectedDb = null;

    function selectDatabase(element) {
        document.querySelectorAll('#db-list li').forEach(function(item) {
            item.classList.remove('selected');
        });
        element.classList.add('selected');
        // Извлекаем имя базы, игнорируя индикатор состояния и спиннер
        selectedDb = element.textContent.trim().split(/\s+/)[0];
        console.log('Выбрана база:', selectedDb);
    }

    function deleteDatabase() {
        if (!selectedDb) {
            alert('Пожалуйста, выберите базу для удаления.');
            return;
        }
        if (confirm('Вы уверены, что хотите удалить базу "' + selectedDb + '"?')) {
            fetch('/delete-db?db=' + encodeURIComponent(selectedDb), { method: 'POST' })
                .then(response => {
                    if (!response.ok) throw new Error('Ошибка удаления базы: ' + response.status);
                    window.location.href = '/';
                })
                .catch(error => {
                    console.error('Ошибка при удалении базы:', error);
                    alert('Ошибка: ' + error.message);
                });
        }
    }

    document.querySelectorAll('#db-list li').forEach(function(item) {
        item.addEventListener('click', function() {
            selectDatabase(this);
        });
        item.addEventListener('dblclick', function() {
            document.getElementById('newDb').value = this.textContent.trim().split(/\s+/)[0];
        });
    });

    document.getElementById('delete-btn')?.addEventListener('click', function() {
        deleteDatabase();
    });

    document.getElementById('restore-form')?.addEventListener('submit', function(event) {
        event.preventDefault();
        const formData = new FormData(this);
        const dbName = formData.get('newDb');

        console.log('Отправка запроса на проверку базы:', dbName);

        fetch('/check-db?db=' + encodeURIComponent(dbName))
            .then(response => {
                if (!response.ok) throw new Error('Ошибка проверки базы: ' + response.status);
                return response.json();
            })
            .then(data => {
                console.log('Результат проверки базы:', data);
                if (data.exists) {
                    console.log('База существует, требуется подтверждение');
                    fetch('/pre-restore-check', {
                        method: 'POST',
                        body: formData
                    })
                    .then(response => {
                        if (!response.ok) throw new Error('Ошибка предварительной проверки: ' + response.status);
                        return response.text();
                    })
                    .then(html => {
                        console.log('Получен HTML для подтверждения');
                        document.open();
                        document.write(html);
                        document.close();
                    })
                    .catch(error => {
                        console.error('Ошибка при предварительной проверке:', error);
                        alert('Ошибка: ' + error.message);
                        resetForm();
                    });
                } else {
                    console.log('База не существует, начинаем восстановление:', dbName);
                    startRestore(dbName);
                    fetch('/restore', {
                        method: 'POST',
                        body: formData
                    })
                    .then(response => {
                        if (!response.ok) throw new Error('Ошибка восстановления: ' + response.status);
                        console.log('Запрос /restore успешен, редирект на /');
                        window.location.href = '/';
                    })
                    .catch(error => {
                        console.error('Ошибка при восстановлении:', error);
                        alert('Ошибка: ' + error.message);
                        resetForm();
                    });
                }
            })
            .catch(error => {
                console.error('Ошибка при проверке базы:', error);
                alert('Ошибка: ' + error.message);
            });
    });

    document.getElementById('confirm-btn')?.addEventListener('click', async function(event) {
        event.preventDefault();
        console.log('Нажата кнопка "Подтвердить"');
        const formData = new FormData(document.getElementById('restore-form'));
        formData.append('confirmRestore', 'true');
        const dbName = formData.get('newDb');
        console.log('Запуск startRestore для базы:', dbName);
        startRestore(dbName);
        try {
            console.log('Отправка запроса /restore');
            const response = await fetch('/restore', {
                method: 'POST',
                body: formData
            });
            if (!response.ok) throw new Error('Ошибка восстановления: ' + response.status);
            console.log('Запрос /restore успешен, редирект на /');
            window.location.href = '/';
        } catch (error) {
            console.error('Ошибка при отправке запроса на восстановление (после подтверждения):', error);
            alert('Ошибка: ' + error.message);
            resetForm();
        }
    });

    var bottomFrame = document.getElementById('bottom-frame');
    if (bottomFrame) {
        bottomFrame.scrollTop = bottomFrame.scrollHeight;
    }

    if (restoringDb) {
        console.log('Имеется восстанавливаемая база:', restoringDb);
        startRestore(restoringDb);
    } else {
        console.log('Нет восстанавливаемой базы при загрузке страницы');
    }
});

function resetForm() {
    console.log('Сброс формы');
    document.getElementById('newDb').value = '';
    document.getElementById('restoreTime').value = '';
    document.getElementById('submit-btn').style.display = 'block';
    document.getElementById('confirmation-section').style.display = 'none';
    document.getElementById('confirm-buttons').style.display = 'none';
    document.getElementById('restore-buttons').style.display = 'none';
    var spinner = document.querySelector('#right-frame h2 .spinner');
    if (spinner) spinner.remove();
    fetch('/cancel', { method: 'POST' })
        .then(response => {
            if (!response.ok) throw new Error('Ошибка отмены: ' + response.status);
            console.log('Запрос /cancel успешен, редирект на /');
            window.location.href = '/';
        })
        .catch(error => {
            console.error('Ошибка при отмене:', error);
            window.location.href = '/';
        });
}

function startRestore(dbName) {
    console.log('Запуск startRestore для базы:', dbName);
    var restoreButtons = document.getElementById('restore-buttons');
    restoreButtons.style.display = 'block';
    restoreButtons.innerHTML = '<button class="cancel-restore-btn" onclick="resetForm()">Отменить восстановление базы данных</button>';
    var submitBtn = document.getElementById('submit-btn');
    var confirmButtons = document.getElementById('confirm-buttons');
    submitBtn.style.display = 'none';
    document.getElementById('confirmation-section').style.display = 'none';
    confirmButtons.style.display = 'none';
    if (!existingDatabases.includes(dbName)) {
        console.log('База не существует, добавляем спиннер');
        var spinner = document.createElement('span');
        spinner.className = 'spinner header-spinner';
        document.querySelector('#right-frame h2').appendChild(spinner);
    } else {
        console.log('База уже существует, спиннер не добавляем');
    }
    pollRestoreStatus();
}

function pollRestoreStatus() {
    console.log('Опрос статуса восстановления');
    fetch('/check-status')
        .then(response => {
            if (!response.ok) throw new Error('Ошибка проверки статуса: ' + response.status);
            return response.json();
        })
        .then(data => {
            console.log('Статус восстановления:', data);
            if (data.completed) {
                console.log('Восстановление завершено');
                var spinner = document.querySelector('#right-frame h2 .spinner');
                if (spinner) spinner.remove();
                document.getElementById('restore-buttons').style.display = 'none';
                window.location.href = '/';
            } else {
                console.log('Восстановление ещё не завершено, повторный опрос через 5 секунд');
                setTimeout(pollRestoreStatus, 5000);
            }
        })
        .catch(error => {
            console.error('Ошибка при опросе статуса:', error);
            alert('Ошибка: ' + error.message);
            resetForm();
        });
}
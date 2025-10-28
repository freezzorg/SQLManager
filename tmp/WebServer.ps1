# WebServer.ps1

Import-Module -Name DBATools -ErrorAction Stop

function New-HomePage {
    param (
        $databases,         # Список баз данных из бэкапов
        $existingDatabases, # Список существующих баз на сервере (массив объектов с Name и Status)
        $messages = @(),    # Сообщения для нижней рамки
        $restoringDb = $null, # Имя восстанавливаемой базы
        $confirmationData = $null # Данные для подтверждения (если требуется)
    )
    $restoringDbValue = if ($restoringDb) { "'$restoringDb'" } else { "null" }
    $existingDatabasesList = $existingDatabases.Name -join ","
    $html = @"
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Восстановление базы данных</title>
    <link rel='icon' type='image/png' href='/favicon.png'>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; padding: 0; background-color: #f0f0f0; }
        #container { display: flex; flex-direction: column; max-width: 1200px; margin: 0 auto; height: calc(100vh - 40px); }
        #main { display: flex; flex: 1; }
        #left-frame, #right-frame { border: 1px solid #ccc; border-radius: 5px; background-color: white; margin: 5px; padding: 10px; }
        #left-frame { width: 25%; max-height: 70vh; display: flex; flex-direction: column; overflow-x: hidden; }
        #right-frame { width: 75%; max-height: 70vh; display: flex; flex-direction: column; }
        #bottom-frame { height: 20%; max-height: 20vh; border: 1px solid #ccc; border-radius: 5px; background-color: white; margin: 5px; padding: 10px; overflow-y: auto; }
        h2 { margin: 0 0 20px 0; font-weight: bold; font-size: 16px; display: flex; align-items: center; justify-content: space-between; }
        ul { list-style: none; padding: 0; margin: 0; flex: 1; overflow-y: auto; overflow-x: hidden; }
        li { padding: 5px 0; cursor: pointer; display: flex; justify-content: space-between; align-items: center; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        li:hover { background-color: #f5f5f5; }
        li.selected { background-color: #d1e7ff; }
        button { padding: 5px 10px; border: none; border-radius: 3px; cursor: pointer; height: 30px; box-sizing: border-box; }
        .refresh-btn { background-color: #2196F3; color: white; }
        .refresh-btn.left { width: 100%; }
        .refresh-btn.right { width: 100px; }
        .refresh-btn:hover { background-color: #1976D2; }
        .delete-btn { background-color: #f44336; color: white; width: 100%; }
        .delete-btn:hover { background-color: #da190b; }
        .clear-btn { background-color: #f44336; color: white; width: 100px; }
        .clear-btn:hover { background-color: #da190b; }
        .button-group { display: flex; gap: 5px; }
        .now-btn { background-color: #FF9800; color: white; width: 100px; }
        .now-btn:hover { background-color: #F57C00; }
        .submit-btn { background-color: #4CAF50; color: white; width: 100%; }
        .submit-btn:hover { background-color: #45a049; }
        .confirm-btn { background-color: #4CAF50; color: white; width: calc(50% - 2.5px); margin-right: 5px; }
        .confirm-btn:hover { background-color: #45a049; }
        .cancel-btn { background-color: #f44336; color: white; width: calc(50% - 2.5px); }
        .cancel-btn:hover { background-color: #da190b; }
        .cancel-restore-btn { background-color: #f44336; color: white; width: 100%; }
        .cancel-restore-btn:hover { background-color: #da190b; }
        #restore-form { width: 100%; flex: 1; display: flex; flex-direction: column; }
        .form-group { width: 100%; display: flex; flex-direction: column; margin-bottom: 10px; }
        label { font-weight: normal; margin-bottom: 5px; }
        select, input[type="text"], input[type="datetime-local"] { width: 100%; padding: 5px; border: 1px solid #ccc; border-radius: 3px; box-sizing: border-box; }
        .input-group { display: flex; align-items: center; gap: 10px; width: 100%; }
        .input-group button { margin-left: auto; }
        .input-group input[type="text"] { width: calc(100% - 110px); }
        .input-group select, .input-group input[type="datetime-local"] { width: calc(100% - 110px); }
        #button-container { margin-top: auto; }
        #confirm-buttons { display: flex; gap: 5px; }
        #messages p { margin: 2px 0; line-height: 1.2; }
        .spinner { border: 3px solid #f3f3f3; border-top: 3px solid #3498db; border-radius: 50%; width: 16px; height: 16px; animation: spin 2s linear infinite; display: inline-block; flex-shrink: 0; margin-left: 5px; }
        .header-spinner { margin-left: auto; }
        .confirmation { margin-top: 10px; padding: 10px; border: 1px solid #ffcc00; background-color: #fff3cd; color: #856404; display: flex; align-items: center; gap: 10px; }
        .status-indicator { width: 10px; height: 10px; border-radius: 50%; display: inline-block; margin-left: 5px; flex-shrink: 0; }
        .status-online { background-color: #4CAF50; }
        .status-offline { background-color: #f44336; }
        .status-restoring { background-color: #FF9800; }
        .status-error { background-color: #9E9E9E; }
        .status-unknown { background-color: #2196F3; }
        @keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
    </style>
    <script type="text/javascript" src="/script.js"></script>
</head>
<body>
    <div id="container">
        <div id="main">
            <div id="left-frame">
                <h2>Базы на сервере</h2>
                <ul id="db-list">
"@
    foreach ($db in $existingDatabases) {
        $dbName = $db.Name
        $status = $db.Status
        $indicator = "<span class='status-indicator status-$status' title='Состояние: $status'></span>"
        if ($dbName -eq $restoringDb) {
            # Если база восстанавливается, показываем только спиннер
            $html += "<li>$dbName<span class='spinner'></span></li>"
        } else {
            # Если база не восстанавливается, показываем только индикатор состояния
            $html += "<li>$dbName$indicator</li>"
        }
    }
    $html += @"
                </ul>
                <div class="button-group">
                    <button id="delete-btn" class="delete-btn">Удалить</button>
                    <button class="refresh-btn left" onclick="window.location.href='/refresh-left'">Обновить</button>
                </div>
            </div>
            <div id="right-frame">
                <h2>Восстановление базы данных</h2>
                <form id="restore-form" method="post">
                    <div class="form-group">
                        <label for="origDb">Выберите базу данных из бэкапа:</label>
                        <div class="input-group">
                            <select name="origDb" id="origDb" required>
"@
    foreach ($db in $databases) {
        $selected = if ($confirmationData -and $confirmationData.origDb -eq $db) { 'selected' } else { '' }
        $html += "<option value='$db' $selected>$db</option>"
    }
    $html += @"
                            </select>
                            <button type="button" class="refresh-btn right" onclick="window.location.href='/refresh'">Обновить</button>
                        </div>
                    </div>
                    <div class="form-group">
                        <label for="newDb">Имя базы данных на сервере:</label>
                        <div class="input-group">
                            <input type="text" name="newDb" id="newDb" required value="$($confirmationData.newDb)">
                            <button type="button" class="clear-btn" onclick="clearNewDb()">Очистить</button>
                        </div>
                    </div>
                    <div class="form-group">
                        <label for="restoreTime">Восстановить на дату и время:</label>
                        <div class="input-group">
                            <input type="datetime-local" name="restoreTime" id="restoreTime" required value="$($confirmationData.restoreTime)">
                            <button type="button" class="now-btn" onclick="setCurrentDateTime()">Сейчас</button>
                        </div>
                    </div>
                    <div id="confirmation-section" style="display: $(if ($confirmationData) { 'block' } else { 'none' })">
                        <div class="confirmation">
                            <label>Внимание: Имя базы в бэкапе ($($confirmationData.origDb)) отличается от имени восстанавливаемой базы ($($confirmationData.newDb)). Вы уверены, что хотите продолжить?</label>
                        </div>
                    </div>
                    <div id="button-container">
                        <button type="submit" class="submit-btn" id="submit-btn" style="display: $(if ($confirmationData) { 'none' } else { 'block' })">Восстановить базу данных</button>
                        <div id="confirm-buttons" style="display: $(if ($confirmationData) { 'flex' } else { 'none' })">
                            <button type="button" class="confirm-btn" id="confirm-btn">Подтвердить</button>
                            <button type="button" class="cancel-btn" onclick="resetForm()">Отменить</button>
                        </div>
                        <div id="restore-buttons" style="display: none;"></div>
                    </div>
                </form>
            </div>
        </div>
        <div id="bottom-frame">
            <div id="messages">
"@
    foreach ($msg in $messages) {
        $html += "<p>$msg</p>"
    }
    $html += @"
            </div>
        </div>
    </div>
    <script>
        var restoringDb = $restoringDbValue;
        var existingDatabases = '$existingDatabasesList'.split(',');
    </script>
</body>
</html>
"@
    return $html
}

function Get-DatabaseList {
    . ./Utils.ps1
    $backupPath = $script:config.backupShare.mountPoint
    try {
        $wasMountedHere = Mount-BackupShare
        $databases = Get-ChildItem -Path $backupPath -Directory | Select-Object -ExpandProperty Name
        Write-Log "Получен список баз: $($databases -join ', ')" -level "DEBUG"
        if ($script:config.forbiddenDatabases) {
            $databases = $databases | Where-Object { $script:config.forbiddenDatabases -notcontains $_ }
            Write-Log "Список баз после исключения запрещённых: $($databases -join ', ')" -level "DEBUG"
        }
        if ($wasMountedHere) { Unmount-BackupShare }
        return $databases
    } catch {
        Write-Log "Ошибка получения списка баз: $($_.Exception.Message)" -level "ERROR"
        if ($wasMountedHere) { Unmount-BackupShare }
        return @()
    }
}

function Get-ExistingDatabases {
    try {
        $sqlPass = ConvertTo-SecureString $script:config.sqlServer.password -AsPlainText -Force
        $sqlCreds = New-Object System.Management.Automation.PSCredential($script:config.sqlServer.username, $sqlPass)
        # Используем список системных баз из конфигурации, если он есть, иначе значения по умолчанию
        $systemDatabases = if ($script:config.systemDatabases) { $script:config.systemDatabases } else { @("master", "tempdb", "model", "msdb") }
        $databases = Get-DbaDatabase -SqlInstance $script:config.sqlServer.instance -SqlCredential $sqlCreds | 
                     Where-Object { $_.Name -notin $systemDatabases }
        
        $result = @()
        foreach ($db in $databases) {
            $dbName = $db.Name
            $state = $db.Status # Состояние базы (Normal, Offline, Restoring, Suspect, Emergency и т.д.)
            
            # Преобразуем состояние базы в упрощённый статус
            $status = switch ($state) {
                "Normal" { "online" }
                "Offline" { "offline" }
                "Restoring" { "restoring" }
                "Recovering" { "restoring" }
                "Suspect" { "error" }
                "Emergency" { "error" }
                default { "unknown" }
            }
            
            # Если база в процессе восстановления через наш скрипт, приоритет имеет $script:restoringDb
            if ($script:restoringDb -and $dbName -eq $script:restoringDb) {
                $status = "restoring"
            }
            
            $result += [PSCustomObject]@{
                Name = $dbName
                Status = $status
            }
            
            Write-Log "База: $dbName, Состояние: $state, Статус для отображения: $status" -level "DEBUG"
        }
        
        Write-Log "Получен список существующих баз (без системных): $($result.Name -join ', ')" -level "DEBUG"
        return $result
    } catch {
        Write-Log "Ошибка получения списка существующих баз: $($_.Exception.Message)" -level "ERROR"
        return @()
    }
}

function Test-DatabaseExists {
    param ($dbName)
    . ./RestoreLogic.ps1
    try {
        $sqlPass = ConvertTo-SecureString $script:config.sqlServer.password -AsPlainText -Force
        $sqlCreds = New-Object System.Management.Automation.PSCredential($script:config.sqlServer.username, $sqlPass)
        $dbState = Get-DbaDbState -SqlInstance $script:config.sqlServer.instance -SqlCredential $sqlCreds -Database $dbName -ErrorAction Stop
        return $null -ne $dbState
    } catch {
        Write-Log "Ошибка проверки базы '$dbName': $($_.Exception.Message)" -level "ERROR"
        return $false
    }
}

function Get-FormData {
    param ($context)
    $result = @{}
    $request = $context.Request
    $contentType = $request.ContentType

    if ($contentType -like "*multipart/form-data*") {
        $boundary = ($contentType -split "boundary=")[1]
        $reader = New-Object System.IO.StreamReader($request.InputStream, $request.ContentEncoding)
        $data = $reader.ReadToEnd()
        Write-Log "Получены данные формы (multipart/form-data): $data" -level "DEBUG"
        $reader.Dispose()

        # Разделяем данные по границе
        $parts = $data -split "--$boundary"
        foreach ($part in $parts) {
            if ($part -match 'Content-Disposition: form-data; name="([^"]+)"\r\n\r\n(.*)\r\n') {
                $name = $matches[1]
                $value = $matches[2]
                $result[$name] = $value
            }
        }
    }
    else {
        $reader = New-Object System.IO.StreamReader($request.InputStream, $request.ContentEncoding)
        $data = $reader.ReadToEnd()
        Write-Log "Получены данные формы (urlencoded): $data" -level "DEBUG"
        $reader.Dispose()
        if ($data) {
            $pairs = $data -split '&'
            foreach ($pair in $pairs) {
                $eq = $pair.IndexOf('=')
                if ($eq -ge 0) {
                    $name = [System.Web.HttpUtility]::UrlDecode($pair.Substring(0, $eq))
                    $value = [System.Web.HttpUtility]::UrlDecode($pair.Substring($eq + 1))
                    $result[$name] = $value
                }
            }
        }
    }
    return $result
}

function Start-WebServer {
    . ./Utils.ps1
    . ./RestoreLogic.ps1

    $configPath = "./config.json"
    if (-not (Test-Path $configPath)) {
        Write-Host "Ошибка: Файл конфигурации $configPath не найден"
        exit 1
    }
    try {
        $script:config = Get-Content $configPath -Raw | ConvertFrom-Json
        Write-Log "Конфигурация загружена: logLevel=$($script:config.logLevel), sqlInstance=$($script:config.sqlServer.instance), mountPoint=$($script:config.backupShare.mountPoint), logFile=$($script:config.logFile), forbiddenDatabases=$($script:config.forbiddenDatabases -join ', '), systemDatabases=$($script:config.systemDatabases -join ', ')" -level "DEBUG"
    } catch {
        Write-Host "Ошибка загрузки конфигурации из ${configPath}: $($_.Exception.Message)"
        exit 1
    }

    $script:databases = Get-DatabaseList
    $script:existingDatabases = Get-ExistingDatabases
    $script:messages = @()
    $script:restoringDb = $null
    Write-Log "Инициализация: restoringDb=$($script:restoringDb)" -level "DEBUG"

    $listener = New-Object System.Net.HttpListener
    $listener.Prefixes.Add("http://+:8080/")
    try {
        $listener.Start()
        Write-Log "Веб-сервер запущен на порту 8080" -level "INFO"
    } catch {
        Write-Log "Ошибка запуска сервера: $($_.Exception.Message)" -level "ERROR"
        return
    }

    $script:running = $true

    trap {
        Write-Log "Trap сработал. Ошибка: $($_.Exception.Message)" -level "ERROR"
        $script:running = $false
        if ($listener.IsListening) {
            $listener.Stop()
            Write-Log "Веб-сервер остановлен в trap" -level "INFO"
        }
        Start-Sleep -Milliseconds 500
        exit 0
    }

    try {
        while ($script:running) {
            Write-Log "Ожидание запроса... restoringDb=$($script:restoringDb)" -level "DEBUG"
            $contextTask = $listener.GetContextAsync()
            while (-not $contextTask.Wait(100) -and $script:running) {}
            if (-not $script:running) { break }

            $context = $contextTask.Result
            $request = $context.Request
            $response = $context.Response
            $clientIP = $request.RemoteEndPoint.Address.ToString()
            Write-Log "Получен запрос: $($request.HttpMethod) $($request.RawUrl) от IP: $clientIP, restoringDb=$($script:restoringDb)" -level "DEBUG"

            # Проверка IP-адреса клиента
            if (-not $script:config.allowedIPs -or $clientIP -notin $script:config.allowedIPs) {
                Write-Log "Доступ запрещён для IP: $clientIP (не в списке allowedIPs)" -level "WARNING"
                $response.StatusCode = 403
                $response.ContentType = "text/html; charset=utf-8"
                $html = "<h1>403 - Доступ запрещён</h1><p>Ваш IP-адрес ($clientIP) не разрешён для доступа к этому сервису.</p>"
                $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                $response.ContentLength64 = $buffer.Length
                $response.OutputStream.Write($buffer, 0, $buffer.Length)
                $response.Close()
                continue
            }

            $response.ContentType = "text/html; charset=utf-8"

            try {
                if ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/") {
                    $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/script.js") {
                    $scriptPath = "./script.js"
                    if (Test-Path $scriptPath) {
                        $response.ContentType = "text/javascript; charset=utf-8"
                        $buffer = [System.IO.File]::ReadAllBytes($scriptPath)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                        Write-Log "Отправлен script.js" -level "DEBUG"
                    } else {
                        $response.StatusCode = 404
                        $html = "<h1>404 - Script не найден</h1>"
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                        Write-Log "Файл script.js не найден по пути $scriptPath" -level "WARNING"
                    }
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/favicon.png") {
                    $faviconPath = "./favicon.png"
                    if (Test-Path $faviconPath) {
                        $response.ContentType = "image/png"
                        $buffer = [System.IO.File]::ReadAllBytes($faviconPath)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                        Write-Log "Отправлен favicon.png" -level "DEBUG"
                    } else {
                        $response.StatusCode = 404
                        $html = "<h1>404 - Favicon не найден</h1>"
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                        Write-Log "Файл favicon.png не найден по пути $faviconPath" -level "WARNING"
                    }
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/refresh") {
                    $script:databases = Get-DatabaseList
                    $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/refresh-left") {
                    $script:existingDatabases = Get-ExistingDatabases
                    $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -eq "/check-status") {
                    Write-Log "Проверка статуса восстановления" -level "DEBUG"
                    $job = Get-Job -State Completed | Where-Object { $_.Command -like "*Invoke-DatabaseRestore*" } | Select-Object -First 1
                    $runningJob = Get-Job -State Running | Where-Object { $_.Command -like "*Invoke-DatabaseRestore*" } | Select-Object -First 1
                    $completed = $null -ne $job
                    Write-Log "Статус задания: Completed=$completed, RunningJob=$($runningJob.Id)" -level "DEBUG"
                    if ($completed) {
                        $script:sqlResult = Receive-Job -Job $job -Keep
                        Remove-Job -Job $job
                        Write-Log "Получен результат из задания: $($script:sqlResult | ConvertTo-Json)" -level "DEBUG"
                        if ($script:sqlResult.Success) {
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp Восстановление базы '$($script:restoringDb)' успешно завершено"
                        } else {
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp Ошибка восстановления: $($script:sqlResult.Message)"
                        }
                        $script:restoringDb = $null
                        $script:existingDatabases = Get-ExistingDatabases # Обновляем список баз
                        Write-Log "Сброс restoringDb, обновлён список баз: $($script:existingDatabases.Name -join ', ')" -level "DEBUG"
                    }
                    $response.ContentType = "application/json; charset=utf-8"
                    $jsonResponse = @{ completed = $completed } | ConvertTo-Json
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($jsonResponse)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "GET" -and $request.RawUrl -match "/check-db") {
                    $dbName = [System.Web.HttpUtility]::ParseQueryString($request.Url.Query)["db"]
                    $exists = $script:existingDatabases.Name -contains $dbName
                    $response.ContentType = "application/json; charset=utf-8"
                    $jsonResponse = @{ exists = $exists } | ConvertTo-Json
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($jsonResponse)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "POST" -and $request.RawUrl -eq "/pre-restore-check") {
                    $formData = Get-FormData -context $context
                    $origDb = $formData["origDb"]
                    $newDb = $formData["newDb"]
                    $restoreTime = $formData["restoreTime"]

                    Write-Log "Предварительная проверка: origDb=$origDb, newDb=$newDb, restoreTime=$restoreTime" -level "DEBUG"

                    $job = Start-Job -ScriptBlock {
                        param($config, $origDb, $newDb, $restoreTime)
                        $script:config = $config
                        . $using:PWD/Utils.ps1
                        . $using:PWD/RestoreLogic.ps1
                        $result = Invoke-DatabaseRestore -origDb $origDb -newDb $newDb -restoreTime $restoreTime
                        return $result
                    } -ArgumentList $script:config, $origDb, $newDb, $restoreTime

                    $job | Wait-Job | Out-Null
                    $result = Receive-Job -Job $job -Keep
                    Remove-Job -Job $job

                    if ($result.Status -eq "ConfirmationRequired") {
                        $confirmationData = @{
                            origDb = $origDb
                            newDb  = $newDb
                            restoreTime = $restoreTime
                        }
                        $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb -confirmationData $confirmationData
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    } else {
                        $response.ContentType = "application/json; charset=utf-8"
                        $jsonResponse = @{
                            status = "Proceed"
                        } | ConvertTo-Json
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($jsonResponse)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    }
                }
                elseif ($request.HttpMethod -eq "POST" -and $request.RawUrl -eq "/restore") {
                    Write-Log "Получен запрос на /restore" -level "DEBUG"
                    $formData = Get-FormData -context $context
                    Write-Log "Данные формы: $($formData | ConvertTo-Json)" -level "DEBUG"
                    $origDb = $formData["origDb"]
                    $newDb = $formData["newDb"]
                    $restoreTime = $formData["restoreTime"]
                    $confirmRestore = $formData["confirmRestore"] -eq "true"
                
                    Write-Log "Получен запрос на восстановление: origDb=$origDb, newDb=$newDb, restoreTime=$restoreTime, confirmRestore=$confirmRestore" -level "INFO"
                
                    if (-not $origDb -or -not $newDb -or -not $restoreTime) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: Все поля должны быть заполнены"
                        Write-Log "Ошибка: Все поля должны быть заполнены" -level "ERROR"
                        $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    }
                    elseif ($script:config.forbiddenDatabases -and $script:config.forbiddenDatabases -contains $origDb) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: База '$origDb' запрещена для восстановления"
                        Write-Log "Ошибка: База '$origDb' запрещена для восстановления" -level "ERROR"
                        $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    }
                    elseif ($script:databases -notcontains $origDb) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: База '$origDb' не найдена в списке доступных баз"
                        Write-Log "Ошибка: База '$origDb' не найдена в списке доступных баз" -level "ERROR"
                        $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    }
                    else {
                        $script:restoringDb = $newDb
                        $restoreDateTime = [DateTime]::Parse($restoreTime).ToString("dd.MM.yyyy HH:mm")
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Начато восстановление базы '$newDb' из бэкапа '$origDb' на $restoreDateTime..."
                        Write-Log "Запуск фонового задания для восстановления" -level "DEBUG"
                        $job = Start-Job -ScriptBlock {
                            param($config, $origDb, $newDb, $restoreTime, $confirmRestore)
                            $script:config = $config
                            Write-Host "Запуск Invoke-DatabaseRestore в фоновом задании"
                            . $using:PWD/Utils.ps1
                            . $using:PWD/RestoreLogic.ps1
                            $result = Invoke-DatabaseRestore -origDb $origDb -newDb $newDb -restoreTime $restoreTime -confirmRestore $confirmRestore
                            Write-Host "Результат Invoke-DatabaseRestore: $($result | ConvertTo-Json)"
                            return $result
                        } -ArgumentList $script:config, $origDb, $newDb, $restoreTime, $confirmRestore
                        Write-Log "Восстановление запущено в фоновом задании (Job ID: $($job.Id))" -level "DEBUG"
                        $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                        $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                        $response.ContentLength64 = $buffer.Length
                        $response.OutputStream.Write($buffer, 0, $buffer.Length)
                    }
                }
                elseif ($request.HttpMethod -eq "POST" -and $request.RawUrl -eq "/cancel") {
                    $job = Get-Job -State Running | Where-Object { $_.Command -like "*Invoke-DatabaseRestore*" } | Select-Object -First 1
                    if ($job) {
                        Stop-Job -Job $job
                        $sqlPass = ConvertTo-SecureString $script:config.sqlServer.password -AsPlainText -Force
                        $sqlCreds = New-Object System.Management.Automation.PSCredential($script:config.sqlServer.username, $sqlPass)
                        try {
                            Remove-DbaDatabase -SqlInstance $script:config.sqlServer.instance -SqlCredential $sqlCreds -Database $script:restoringDb -Confirm:$false
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp База '$script:restoringDb' удалена"
                            Write-Log "База '$script:restoringDb' успешно удалена" -level "INFO"
                        } catch {
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp Ошибка удаления базы '$script:restoringDb': $($_.Exception.Message)"
                            Write-Log "Ошибка удаления базы '$script:restoringDb': $($_.Exception.Message)" -level "ERROR"
                        }
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Восстановление базы '$script:restoringDb' отменено"
                        Remove-Job -Job $job
                        Write-Log "Восстановление базы '$script:restoringDb' отменено" -level "INFO"
                    }
                    $script:restoringDb = $null
                    $script:existingDatabases = Get-ExistingDatabases # Обновляем список баз после отмены
                    $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                elseif ($request.HttpMethod -eq "POST" -and $request.RawUrl -match "/delete-db") {
                    $dbName = [System.Web.HttpUtility]::ParseQueryString($request.Url.Query)["db"]
                    Write-Log "Получен запрос на удаление базы: $dbName" -level "INFO"

                    if (-not $dbName) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: Не указано имя базы для удаления"
                        Write-Log "Ошибка: Не указано имя базы для удаления" -level "ERROR"
                    }
                    elseif ($script:restoringDb -eq $dbName) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: Нельзя удалить базу '$dbName', так как она в процессе восстановления"
                        Write-Log "Ошибка: Нельзя удалить базу '$dbName', так как она в процессе восстановления" -level "ERROR"
                    }
                    elseif ($script:existingDatabases.Name -notcontains $dbName) {
                        $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                        $script:messages += "$timestamp Ошибка: База '$dbName' не найдена"
                        Write-Log "Ошибка: База '$dbName' не найдена" -level "ERROR"
                    }
                    else {
                        $sqlPass = ConvertTo-SecureString $script:config.sqlServer.password -AsPlainText -Force
                        $sqlCreds = New-Object System.Management.Automation.PSCredential($script:config.sqlServer.username, $sqlPass)
                        try {
                            Remove-DbaDatabase -SqlInstance $script:config.sqlServer.instance -SqlCredential $sqlCreds -Database $dbName -Confirm:$false
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp База '$dbName' успешно удалена"
                            Write-Log "База '$dbName' успешно удалена" -level "INFO"
                        } catch {
                            $timestamp = (Get-Date).ToString("dd.MM.yyyy HH:mm:ss")
                            $script:messages += "$timestamp Ошибка удаления базы '$dbName': $($_.Exception.Message)"
                            Write-Log "Ошибка удаления базы '$dbName': $($_.Exception.Message)" -level "ERROR"
                        }
                        $script:existingDatabases = Get-ExistingDatabases # Обновляем список баз после удаления
                    }
                    $html = New-HomePage -databases $script:databases -existingDatabases $script:existingDatabases -messages $script:messages -restoringDb $script:restoringDb
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
                else {
                    $response.StatusCode = 404
                    $html = "<h1>404 - Страница не найдена</h1>"
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
            }
            catch {
                Write-Log "Ошибка обработки запроса от IP: $clientIP : $($_.Exception.Message)" -level "ERROR"
                if ($response.OutputStream.CanWrite) {
                    $response.StatusCode = 500
                    $html = "<h1>500 - Ошибка: $($_.Exception.Message)</h1>"
                    $buffer = [System.Text.Encoding]::UTF8.GetBytes($html)
                    $response.ContentLength64 = $buffer.Length
                    $response.OutputStream.Write($buffer, 0, $buffer.Length)
                }
            }
            finally {
                $response.Close()
            }
        }
    }
    finally {
        if ($listener.IsListening) {
            $listener.Stop()
        }
        if ($script:wasMounted) { Unmount-BackupShare }
        Write-Log "Веб-сервер остановлен" -level "INFO"
    }
}

Start-WebServer
# build-android.ps1 - the real implementation of the mobile APK build
# (build_android.bat is only a 5-line launcher).
#
# Why PowerShell: cmd.exe misparses LF-only batch files and can eat the first
# character of a line (symptom: "REM xxx" runs as "EM xxx" -> "'EM' is not
# recognized as an internal or external command"). A .ps1 is immune, and it can
# also convert the repo's .bat files to CRLF as a side effect.
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 decodes a BOM-less
# script with the ANSI code page; non-ASCII text would be garbled or break
# parsing.
#
# Usage:
#   build_android.bat              # debug APK
#   build_android.bat release      # release APK (debug signing, installable)
#   build_android.bat clean        # wipe Gradle caches, then debug
#   build_android.bat clean release
#   build_android.bat --info       # passed through to gradle (--offline etc.)
#   powershell -File build-android.ps1 -RepairCacheOnly    # cache repair only

param([switch]$RepairCacheOnly)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot

# ---- arguments -------------------------------------------------------
$wantRelease = $false
$wantClean = $false
$gradleExtra = @()
foreach ($a in $args) {
    switch -Regex ($a) {
        '^(?i)release$' { $wantRelease = $true }
        '^(?i)clean$'   { $wantClean = $true }
        '^-'            { $gradleExtra += $a }
        default         { }
    }
}

$task = if ($wantRelease) { 'assembleRelease' } else { 'assembleDebug' }
$apkDir = if ($wantRelease) { 'release' } else { 'debug' }
$apkFile = if ($wantRelease) { 'app-release.apk' } else { 'app-debug.apk' }

function Fail($msg) {
    Write-Host ""
    Write-Host "  ERROR: $msg" -ForegroundColor Red
    exit 1
}

function Step($n, $msg) {
    Write-Host "[$n/5] $msg"
}

# ---- environment: Go / JDK -------------------------------------------
# Nothing here is machine specific: Go comes from PATH (or GO_BIN / the usual
# install folders), the JDK is discovered from JAVA_HOME / PATH / Android
# Studio / the vendor folders.
if (-not $env:GOPROXY) { $env:GOPROXY = 'https://goproxy.cn,direct' }

# GO_BIN lets a user point at a Go install that is not on PATH.
if ($env:GO_BIN -and (Test-Path (Join-Path $env:GO_BIN 'go.exe'))) {
    $env:PATH = "$($env:GO_BIN);$env:PATH"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    # Common install locations, including the "keep tools in my home folder"
    # habit (~/tools/go). Missing folders are simply skipped.
    $goCandidates = @(
        (Join-Path $env:USERPROFILE 'tools\go\bin'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Go\bin'),
        (Join-Path $env:ProgramFiles 'Go\bin'),
        (Join-Path $env:USERPROFILE 'go\bin'),
        (Join-Path $env:USERPROFILE 'scoop\shims'),
        (Join-Path $env:ProgramData 'chocolatey\bin')
    )
    foreach ($c in $goCandidates) {
        if ((Test-Path (Join-Path $c 'go.exe'))) { $env:PATH = "$c;$env:PATH"; break }
    }
}

# JAVA_HOME is often stored WITH surrounding quotes (people run
#   set JAVA_HOME="C:\path\to\jdk"
# which keeps the quotes in the value). Join-Path then treats '"C' as a drive
# name and fails, so normalize before use. Same for the SDK variables.
function ConvertTo-CleanPath($value) {
    if (-not $value) { return $null }
    return $value.Trim().Trim('"').TrimEnd('\')
}

$jh = ConvertTo-CleanPath $env:JAVA_HOME
if (-not ($jh -and (Test-Path (Join-Path $jh 'bin\java.exe')))) { $jh = $null }

if (-not $jh) {
    # Android Studio ships a full JDK (JBR) - present on most machines that
    # already build Android apps.
    $studioCandidates = @(
        (Join-Path $env:ProgramFiles 'Android\Android Studio\jbr'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Android Studio\jbr')
    )
    foreach ($c in $studioCandidates) {
        if ((-not $jh) -and (Test-Path (Join-Path $c 'bin\java.exe'))) { $jh = $c }
    }
}

if (-not $jh) {
    # Vendor JDKs installed the usual way; newest name wins (jdk-21 > jdk-17).
    $vendorRoots = @(
        (Join-Path $env:ProgramFiles 'Eclipse Adoptium'),
        (Join-Path $env:ProgramFiles 'Java'),
        (Join-Path $env:ProgramFiles 'Microsoft'),
        (Join-Path $env:ProgramFiles 'Zulu'),
        (Join-Path $env:ProgramFiles 'Amazon Corretto'),
        (Join-Path $env:ProgramFiles 'BellSoft')
    )
    foreach ($root in $vendorRoots) {
        if ($jh) { break }
        $found = @(Get-ChildItem -Path $root -Directory -ErrorAction SilentlyContinue |
            Where-Object { Test-Path (Join-Path $_.FullName 'bin\java.exe') } |
            Sort-Object Name -Descending)
        if ($found.Count -gt 0) { $jh = $found[0].FullName }
    }
}

if ($jh) {
    $env:JAVA_HOME = $jh
    $env:PATH = "$jh\bin;$env:PATH"
} else {
    Remove-Item Env:\JAVA_HOME -ErrorAction SilentlyContinue
}

# ---- Gradle cache repair ---------------------------------------------
# A cache entry that lost metadata.bin makes every later build fail with
#   "Could not read workspace metadata from ...\metadata.bin"
# Only the broken entries are deleted; dependency jars in modules-2 stay, so
# nothing is re-downloaded.
function Repair-GradleCache {
    $cache = Join-Path $env:USERPROFILE '.gradle\caches'
    if (-not (Test-Path $cache)) { return }

    Write-Host "  scanning Gradle cache $cache ..."
    $entries = New-Object System.Collections.ArrayList

    # 1) artifact transforms: caches\transforms-<n>\<hash>
    Get-ChildItem -Path (Join-Path $cache 'transforms-*') -Directory -ErrorAction SilentlyContinue | ForEach-Object {
        Get-ChildItem -LiteralPath $_.FullName -Directory -ErrorAction SilentlyContinue | ForEach-Object {
            [void]$entries.Add($_)
        }
    }
    # 2) compiled Kotlin DSL scripts: caches\<ver>\kotlin-dsl\scripts\<hash>
    Get-ChildItem -Path (Join-Path $cache '*\kotlin-dsl\scripts') -Directory -ErrorAction SilentlyContinue | ForEach-Object {
        Get-ChildItem -LiteralPath $_.FullName -Directory -ErrorAction SilentlyContinue | ForEach-Object {
            [void]$entries.Add($_)
        }
    }
    # 3) compiled build/init scripts: caches\<ver>\scripts*\<hash>
    Get-ChildItem -Path (Join-Path $cache '*\scripts*') -Directory -ErrorAction SilentlyContinue | ForEach-Object {
        Get-ChildItem -LiteralPath $_.FullName -Directory -ErrorAction SilentlyContinue | ForEach-Object {
            [void]$entries.Add($_)
        }
    }

    $bad = @($entries | Where-Object { -not (Test-Path (Join-Path $_.FullName 'metadata.bin')) })
    if ($bad.Count -eq 0) {
        Write-Host "    nothing broken found"
        return
    }

    Write-Host ("    found " + $bad.Count + " broken cache entries, cleaning ...")
    foreach ($e in $bad) {
        Remove-Item -LiteralPath $e.FullName -Recurse -Force -ErrorAction SilentlyContinue
    }
    $left = @($bad | Where-Object { Test-Path $_.FullName })
    if ($left.Count -eq 0) {
        Write-Host "    cleaned"
    } else {
        Write-Host ("    WARN: " + $left.Count + " entries could not be deleted (locked). Close Android Studio and retry.") -ForegroundColor Yellow
    }
}

if ($RepairCacheOnly) {
    Repair-GradleCache
    exit 0
}

Write-Host "============================================"
Write-Host "  wb2api mobile - build"
Write-Host "============================================"
Write-Host ""

# ---- [1/5] Go ---------------------------------------------------------
Step 1 'Checking Go ...'
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    Fail 'go not found. Install Go 1.22+ and add it to PATH, or set GO_BIN to the folder holding go.exe, e.g.: setx GO_BIN C:\path\to\go\bin'
}
Write-Host ("    " + (& go version))

# ---- [2/5] Android SDK ------------------------------------------------
Step 2 'Checking Android SDK ...'
$haveSdk = $false
if (Test-Path (Join-Path $Root 'mobile\local.properties')) { $haveSdk = $true }
$sdkHome = ConvertTo-CleanPath $env:ANDROID_HOME
if (-not $sdkHome) { $sdkHome = ConvertTo-CleanPath $env:ANDROID_SDK_ROOT }
if ($sdkHome) {
    $env:ANDROID_HOME = $sdkHome
    $haveSdk = $true
}
if (-not $haveSdk) {
    $def = Join-Path $env:LOCALAPPDATA 'Android\Sdk'
    if (Test-Path (Join-Path $def 'platform-tools\adb.exe')) {
        $env:ANDROID_HOME = $def
        $haveSdk = $true
    }
}
if (-not $haveSdk) {
    Fail "Android SDK not found. Set ANDROID_HOME, or create mobile\local.properties containing: sdk.dir=C:/path/to/Android/Sdk"
}
Write-Host "    SDK found"

# ---- [3/5] JDK --------------------------------------------------------
Step 3 'Checking JDK ...'
$java = Get-Command java -ErrorAction SilentlyContinue
if (-not $java) { Fail 'java not found. Install JDK 17 and add it to PATH.' }
Write-Host "    JDK found"

# ---- [4/5] Go engine (task mode, android/arm64) ----------------------
Step 4 'Building Go engine ...'
$jni = Join-Path $Root 'mobile\app\src\main\jniLibs\arm64-v8a'
New-Item -ItemType Directory -Force -Path $jni | Out-Null
$soPath = Join-Path $jni 'libwb2api.so'

$env:CGO_ENABLED = '0'
$env:GOOS = 'android'
$env:GOARCH = 'arm64'
Push-Location $Root
try {
    & go build -trimpath -ldflags '-s -w' -o $soPath ./cmd/server
    $goCode = $LASTEXITCODE
} finally {
    Pop-Location
}
if ($goCode -ne 0) { Fail 'Go cross-build failed, see the log above.' }
if (-not (Test-Path $soPath)) { Fail "libwb2api.so was not produced at $soPath" }
Write-Host ("    engine built: " + (Get-Item $soPath).Length + " bytes")

# ---- cache repair (cheap, runs before every build) --------------------
if ($wantClean) {
    Write-Host "  cleaning Gradle caches ..."
    # 必须先停守护进程：Windows 上 Gradle daemon 持有 transforms-4 里的 jar 句柄，
    # 边删边跑会把缓存删成"索引还在、文件没了"的坏状态，下一次构建直接 metadata 读失败。
    $stopWrapper = Join-Path $Root 'mobile\gradlew.bat'
    if (Test-Path $stopWrapper) { & $stopWrapper --stop 2>$null | Out-Null }
    Repair-GradleCache
    $cachesRoot = Join-Path $env:USERPROFILE '.gradle\caches'
    Get-ChildItem -Path (Join-Path $cachesRoot 'transforms-*') -Directory -ErrorAction SilentlyContinue |
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $Root 'mobile\.gradle') -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $Root 'mobile\app\build') -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host "    done"
} else {
    Repair-GradleCache
}

# ---- [5/5] Gradle -----------------------------------------------------
# Gradle 会在旧 APK 上做增量重写：条目位置变化时把旧数据抹成 0，留下真实空洞
# （产物可能 7.7MB，实际内容只有 3.9MB）。先删旧产物，让 packaging 从零写。
$apkOutDir = Join-Path $Root 'mobile\app\build\outputs\apk'
$apkOutDir = Join-Path $apkOutDir $apkDir
if (Test-Path $apkOutDir) {
    Get-ChildItem -Path $apkOutDir -Filter *.apk -ErrorAction SilentlyContinue |
        Remove-Item -Force -ErrorAction SilentlyContinue
}

Step 5 "Building APK ($task) ..."
$gradle = $null
$localWrapper = Join-Path $Root 'mobile\gradlew.bat'
if (Test-Path $localWrapper) {
    # Preferred: the wrapper from this repo pins the Gradle version for everyone.
    $gradle = $localWrapper
} else {
    $onPath = Get-Command gradle -ErrorAction SilentlyContinue
    if ($onPath) { $gradle = $onPath.Source }
}
if (-not $gradle) {
    Fail @'
gradle not found.
  Fix it any of these ways:
    - open the mobile\ folder in Android Studio once (it adds gradlew), or
    - install Gradle 8.7+ and add it to PATH, or
    - run "gradle wrapper" inside mobile\ (pins 8.7, see gradle\wrapper\gradle-wrapper.properties)
'@
}

Write-Host "    gradle: $gradle"

# Version: single source of truth is appVersion in cmd/server/main.go (e.g.
# "1.16.1-panel"). Strip the suffix and inject it into Gradle so the desktop
# engine and the APK can never drift apart.
$appVersion = '1.16.1'
try {
    $mainGo = Get-Content (Join-Path $Root 'cmd\server\main.go') -Raw
    $m = [regex]::Match($mainGo, 'const\s+appVersion\s*=\s*"([^"]+)"')
    if ($m.Success) { $appVersion = $m.Groups[1].Value -replace '-panel$', '' }
} catch { }
Write-Host "    version: $appVersion (from cmd/server/main.go)"

Write-Host "    note: first run resolves AGP from Maven - may take a few minutes."
Write-Host ""

Push-Location (Join-Path $Root 'mobile')
try {
    & $gradle --console=plain $task "-PappVersion=$appVersion" @gradleExtra
    $gradleCode = $LASTEXITCODE
} finally {
    Pop-Location
}
if ($gradleCode -ne 0) {
    Write-Host ""
    Write-Host "  ERROR: Android build failed, see the log above." -ForegroundColor Red
    Write-Host "  If it says ""Could not read workspace metadata"", run:"
    Write-Host "      build_android.bat clean"
    Write-Host "  Other causes: JDK not 17, SDK platform 34 missing, AGP/Gradle mismatch."
    exit 1
}

# ---- collect the APK --------------------------------------------------
$apk = Join-Path $Root ("mobile\app\build\outputs\apk\" + $apkDir + "\" + $apkFile)
if (-not (Test-Path $apk)) { Fail "$apk was not produced." }
$distDir = Join-Path $Root 'dist\android'
New-Item -ItemType Directory -Force -Path $distDir | Out-Null
$distApk = Join-Path $distDir 'wb2api-tasks.apk'
Copy-Item -LiteralPath $apk -Destination $distApk -Force
$sizeKb = [int]((Get-Item $distApk).Length / 1KB)

Write-Host ""
Write-Host "============================================"
Write-Host "  BUILD OK"
Write-Host "============================================"
Write-Host "  APK: dist\android\wb2api-tasks.apk"
Write-Host "       size: $sizeKb KB (engine is compressed inside the APK and unpacked on install; about 3.8MB is normal, under 1MB means the engine is missing)"
if (Get-Command adb -ErrorAction SilentlyContinue) {
    Write-Host "  Install: adb install -r dist\android\wb2api-tasks.apk"
}
Write-Host ""
Write-Host "  First run inside the app:"
Write-Host "    - allow notifications (the foreground service needs it)"
Write-Host "    - gear (bottom right) - battery whitelist, then Add Account"
Write-Host "    - account login opens the system browser (OAuth), then come back"
Write-Host ""

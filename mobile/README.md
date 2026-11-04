# 手机端 App（Android，任务模式）

把本项目做成一个**独立手机应用**：桌面图标点开就是面板，签到 / 成长任务 / 活跃上报 /
旅行 / 夜猫子 / 保活全部在手机上按点自动执行 —— **不需要电脑**，也不需要电脑上跑
`wb2api.exe`。

> ⚠️ 这个 App **不提供反代**。它跑的引擎带 `-mode=tasks`：不注册 `/v1/*` 任何路由，
> 只监听 `127.0.0.1`，对外什么都不暴露。想给其他工具提供 OpenAI 兼容接口，仍然在电脑
> 或服务器上跑完整的 `wb2api`。

## 它是什么

一个很薄的 Android 壳 + 完整的 Go 引擎：

```
APK
├── lib/arm64-v8a/libwb2api.so     ← 交叉编译出来的 Go 引擎（-mode=tasks）
└── 壳（Java）
    ├── EngineService   前台服务，拉起/守护引擎进程
    ├── PulseReceiver   15 分钟一次的心跳闹钟（穿透 Doze）
    ├── BootReceiver    开机自启
    └── MainActivity    WebView 加载 http://127.0.0.1:7863/panel/
```

- Go 可执行文件被命名成 `lib*.so` 放进 `jniLibs`：Android 10+ 的 W^X 限制禁止从应用
  可写目录 exec，只有 `nativeLibraryDir`（安装时从 APK 解压出来的这个目录）可执行。
- 界面直接复用现有面板，包括 `mobile.css` / `mobile.js` 的手机适配层，所以手机上看到
  的就是卡片化的面板，与桌面端功能一比一。

## 前置环境（打包机）

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | 1.22+ | 交叉编译 `GOOS=android GOARCH=arm64` |
| JDK | **17** | AGP 8.5 硬性要求，低版本会直接报错 |
| Android SDK | Platform 34 + Build-Tools 34.x | `ANDROID_HOME` 指向它 |
| Gradle | 8.7+ | 或用 Android Studio 打开 `mobile/` 目录 |

没有 SDK 的话，最快的路子是装 [Android Studio](https://developer.android.com/studio)，
把 `mobile/` 目录当项目打开，等 Gradle Sync 完成后 Build → Build APK。

## 构建

```bash
# Windows：一键（检查 Go / SDK / JDK → 交叉编译引擎 → gradle 打包 → 拷贝 APK）
build_android.bat            # 仓库根目录；debug 包
build_android.bat release    # release 包（debug 签名，同样可直接装）
build_android.bat clean      # 清 Gradle 缓存后再打包（解决下面这类脏缓存报错）

# macOS / Linux / Git Bash
./mobile/build-apk.sh
```

> 版本号：APK 的 `versionName` 由构建脚本从 `cmd/server/main.go` 的 `appVersion`
> 读出并注入（日志里打印 `version: x.y.z (from cmd/server/main.go)`），与桌面端
> 永远一致。`app/build.gradle` 里的值是给 Android Studio 直接构建用的兜底值。
>
> 实现细节：`build_android.bat` 只是启动器（7 行，没有 `REM`），真正逻辑在仓库根的
> `build-android.ps1`（纯 ASCII，PowerShell 5.1 自带）。行尾由仓库根的
> `.gitattributes` 统一规定（`*.bat` = CRLF、`*.sh` = LF），clone 下来就是对的，
> 不需要手工转换。

> **`Could not read workspace metadata from ...\metadata.bin`**
> Gradle 缓存条目残缺（常见于上次构建被中断，或与其它项目/版本共用同一个
> `~\.gradle` 缓存）。每次构建前会自动扫描并只删掉残缺条目（`transforms-*`、
> `kotlin-dsl\scripts`、`scripts*` 三类），也可以单独跑 `clean-gradle-cache.bat`。
> 依赖 jar 在 `modules-2` 里不动，不会重新下载。

产物：

- `dist/android/wb2api-tasks.apk` —— 脚本拷出来的成品（方便直接传手机）
- `mobile/app/build/outputs/apk/debug/app-debug.apk` —— gradle 原始输出

装到手机：

```bash
adb install -r dist/android/wb2api-tasks.apk
# 或者把 APK 传到手机，点击安装（需允许「安装未知来源应用」）
```

第一次构建会比较慢（Gradle 要下载 AGP 与 SDK 组件）。脚本按 `[1/5] …[5/5]` 打印每一步，
哪一步挂了就看那一行；任何一步失败都会停下来并提示具体原因（Go 缺失 / SDK 未配置 /
JDK 不是 17 / gradle 缺失 / 产物未生成）。

> 脚本不写死任何本机路径，按下面的顺序自动找，找不到才报错：
>
> | 依赖 | 查找顺序 |
> |---|---|
> | Go | `GO_BIN` 环境变量 → `PATH` 里的 `go` → `%LOCALAPPDATA%\Programs\Go\bin` / `%ProgramFiles%\Go\bin` / `%USERPROFILE%\go\bin` |
> | Android SDK | `mobile\local.properties` → `ANDROID_HOME` → `ANDROID_SDK_ROOT` → `%LOCALAPPDATA%\Android\Sdk` |
> | JDK | `JAVA_HOME`（自动去掉多余的引号）→ Android Studio 自带 JBR → Adoptium / Java / Microsoft / Zulu / Corretto / BellSoft 目录下最新的 JDK |
> | Gradle | `mobile\gradlew(.bat)` → `PATH` 里的 `gradle` → 都没有则提示安装方式 |
>
> 仓库已包含 gradle wrapper（`gradlew` / `gradlew.bat` / `gradle-wrapper.jar`），
> 版本固定为 8.7、下载源指向腾讯镜像，**不需要预先安装 Gradle**：
> `gradle\wrapper\gradle-wrapper.properties` 里可改版本或镜像地址。

## 把电脑上的账号搬到手机

手机端有自己独立的账号目录（应用私有目录 `files/engine/auths/`），与电脑端的
`auths/` 不共享。两种搬法，任选其一。

### 方式 A：导出 → 手机面板「导入」（不需要 adb）

两种导出方式等价，**都包含备注（note）与凭证**：

- **面板按钮（推荐）**：任意端打开面板 → 添加账号 → 导入 → 点 **「导出本机账号」** →
  「下载文件」或「复制」（手机 WebView 里下载可能无效，用「复制」存成 .json 即可）；
- **命令行**：`python scripts/export_cockpit.py`（读 `./auths`，写出 `cockpit-accounts.json`）。

2. 把这个 JSON 传到手机（USB / 微信文件传输 / 网盘都行）；
3. 手机 App → **添加账号** → 切到 **导入** 标签 → 选文件 → 点 **「导入账号」** →
   看到「导入完成：成功 N 个」后点 **「完成」**。

> **备注跟着账号走**：备注写在 `auths/workbuddy-*.json` 的顶层 `note` 键里（面板「账号备注」编辑），
> 所以复制 `auths` 目录、或用上面的导出/导入换机器时都不会丢。早期版本存在浏览器
> localStorage 里的备注，会在面板首次加载时自动迁移进账号文件。
   导入成功后面板会显示账号，并顺手做一次签到。

> `cockpit-accounts.json` 里是**明文** accessToken / refreshToken，等同于账号本身。
> 传完立刻删掉本地副本，不要留在聊天记录或公共网盘里。

### 方式 B：adb 直接拷（debug 包，最快）

```bat
adb shell run-as com.wb2api.tasks mkdir -p files/engine/auths
for %f in (auths\*.json) do adb shell "run-as com.wb2api.tasks sh -c 'cat > files/engine/auths/%~nxf'" < "%f"
```

拷完在 App 里 **齿轮 → 重启引擎**（引擎只在启动时扫描 auths 目录）。

> `run-as` 只对 debuggable 包有效；`build_android.bat` 产出的 debug APK 满足条件。
> release 包同样是 debug 签名，但 `debuggable` 取决于构建类型，release 下 run-as 会拒绝。

### 搬完之后

- **同一个账号不要在两端同时开定时任务**：会重复签到 / 重复上报，容易触发上游风控。
  建议手机端接管日常任务，电脑端把 `schedule.*_enabled` 全部关掉，只保留反代。
- 手机端的 token 过期后由引擎自动 refresh，刷新后的凭证只存在手机上，不会回写电脑端。

## 第一次使用

1. 打开 App → 引擎自动启动 → 面板出现（首次是空的账号池）；
2. 点右上角 **添加账号** → 点「打开授权页」→ **跳到系统浏览器**完成腾讯授权
   （OAuth 必须走真实浏览器，WebView 里登录容易触发风控）；
3. 授权完成后回到 App，面板会自动轮询到结果并把账号载入池中，同时做一次签到；
4. 之后什么都不用管：前台服务 + 心跳闹钟会让它按 `config.json` 里的时间点自动跑任务。

面板里的时间点、任务开关、DNS 等都在 **配置** 页，改完即时生效。

## 右下角齿轮

面板是全屏网页，引擎相关的操作放在右下角半透明齿轮里（尽量不挡内容）：

| 菜单项 | 作用 |
|---|---|
| 重启引擎 | 改完配置/端口后手动重启 |
| 在浏览器中打开面板 | 用系统浏览器访问 `127.0.0.1:7863/panel/` |
| 引擎日志 | 最近 300 行引擎输出（可复制），排查启动失败 |
| 加入电池优化白名单 | 跳系统设置，**强烈建议点**，否则国产 ROM 会几小时后杀掉进程 |
| 设置（端口 / 时区） | 端口与 `TZ`；时区填错会导致签到按 UTC 钟点触发 |
| 停止引擎并退出 | 彻底停掉（同时取消心跳闹钟） |

## 保活与耗电

三层机制，缺一层都可能「半夜不跑」：

1. **前台服务**（`dataSync` 类型）：进程不被系统回收，通知栏常驻，可一键停止；
2. **心跳闹钟**：`setAndAllowWhileIdle` 每 15 分钟唤醒一次并持 90 秒 wakelock ——
   Doze 会冻结整个进程，Go 的 ticker 不前进，不唤醒就会把「9:00 签到」推迟到下次点亮屏幕；
3. **开机自启**：重启手机后自动拉起（可在代码里关：`Prefs.autostart()`）。

耗电：每 15 分钟醒一次，实测对续航影响很小，但如果手机特别省电取向，可以把
`PulseReceiver.INTERVAL_MS` 调大（代价是任务触发时间变糙）。

> 国产 ROM（MIUI / HyperOS / EMUI / ColorOS / OriginOS）还需要在系统设置里额外允许
> **自启动**、**后台运行**、**省电策略设为无限制**，否则上述机制都会被厂商策略覆盖。

## 和电脑端的关系

手机端有**自己独立的账号与状态**（`auths/`、`state.json`、`usage.json` 都在 App 私有目录里，
路径 `<应用数据>/engine/`），与电脑上那份完全无关。

⚠️ **同一个账号不要在两端同时开定时任务** —— 会重复签到/重复上报，容易触发上游风控。
建议：手机端接管日常任务，电脑端只保留反代（把它的 `schedule.*_enabled` 关掉）。

## 故障排查

| 现象 | 原因 / 处理 |
|---|---|
| 打包时 `[1/5] ERROR: go not found` | Go 不在 PATH。把 Go 的 `bin` 目录写进环境变量后重开窗口：`setx GO_BIN "C:\path\to\go\bin"`（脚本也会自动找 `%USERPROFILE%\tools\go\bin`、`%LOCALAPPDATA%\Programs\Go\bin`、`%ProgramFiles%\Go\bin`、scoop/chocolatey 的 shims） |
| 一直停在「正在启动任务引擎…」 | 点齿轮 → 引擎日志；多为 APK 里没有内嵌二进制（构建脚本没跑）或手机不是 arm64 |
| 面板能开但所有域名请求失败 | 手机没有 `/etc/resolv.conf`，`config.json` 的 `dns_servers` 必须显式配（首次运行已写入 223.5.5.5 / 119.29.29.29） |
| 签到时间不对（晚 8 小时） | `TZ` 不是有效 IANA 名称 → 齿轮 → 设置里改成 `Asia/Shanghai` |
| 面板 401 | `api_key` 被填写了。任务模式的 `config.json` 里应为 `""`（App 首次运行就是这样写的） |
| 装不上 / 解析包错误 | ABI 只打了 `arm64-v8a`；极老设备（32 位）需要在 `app/build.gradle` 的 `abiFilters` 里加 `armeabi-v7a`，并在构建脚本里多编译一份 |
| Android 15 上被系统掐掉 | targetSdk 已停在 34 规避 dataSync 的 6 小时上限；若仍受限，检查是否被系统「自动撤销权限」 |

## 风险声明

本项目仅限**自用账号**（签到 / 保活 / 个人工具接入），禁止批量小号、倒卖额度、二次打包
收费分发 —— 详见仓库根目录 README 的《使用声明》。手机端与桌面端承担同样的风险：
上游随时可能收紧风控，账号风控 / 封禁由使用者自行承担。

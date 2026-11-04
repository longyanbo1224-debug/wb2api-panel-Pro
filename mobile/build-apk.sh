#!/usr/bin/env bash
# 一键构建手机端 APK（Linux / macOS / Git Bash）。
#
#   ./mobile/build-apk.sh             → debug APK（可直接装）
#   ./mobile/build-apk.sh --release   → release APK（同样是 debug 签名，可直接装）
#
# 做三件事：
#   1) 把 cmd/server 交叉编译成 Android/arm64 可执行文件，改名 libwb2api.so；
#   2) 放进 mobile/app/src/main/jniLibs/arm64-v8a/（构建产物，不进版本库）；
#   3) 调 gradle 打包。
#
# 前置：Go 1.22+、JDK 17、Android SDK（compileSdk 34 / build-tools 34.x）。
#       Gradle 8.7+：如果 mobile/ 下有 gradlew 就用它，否则用 PATH 里的 gradle。
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
JNI="$ROOT/mobile/app/src/main/jniLibs/arm64-v8a"
APK_DIR="$ROOT/mobile/app/build/outputs/apk"

die() { echo "错误：$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "找不到 go，请先安装 Go 1.22+"
command -v java >/dev/null 2>&1 || die "找不到 java，请先安装 JDK 17（AGP 8.5 要求）"

if [ -z "${ANDROID_HOME:-}" ] && [ -z "${ANDROID_SDK_ROOT:-}" ]; then
  echo "警告：未设置 ANDROID_HOME / ANDROID_SDK_ROOT。"
  echo "      gradle 需要在 mobile/local.properties 里找到 sdk.dir，否则会报"
  echo "      'SDK location not found'。可执行："
  echo "        echo \"sdk.dir=/你的/Android/Sdk\" > mobile/local.properties"
fi

echo "==> 1/3 交叉编译 Go 引擎（GOOS=android GOARCH=arm64，任务模式）"
mkdir -p "$JNI"
# -trimpath: 去掉本机路径，产物可复现
# -s -w    : 去掉符号表与调试信息，体积减半（9MB → 约 6MB）
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "-s -w" \
  -o "$JNI/libwb2api.so" ./cmd/server
ls -lh "$JNI/libwb2api.so"

echo "==> 2/3 检查产物"
if command -v file >/dev/null 2>&1; then
  file "$JNI/libwb2api.so" || true
fi
head -c 4 "$JNI/libwb2api.so" | od -c | head -1

echo "==> 3/3 gradle 打包"
cd "$ROOT/mobile"
if [ -x ./gradlew ]; then
  GRADLE=./gradlew
elif command -v gradle >/dev/null 2>&1; then
  GRADLE=gradle
else
  die "找不到 gradle（需要 8.7+），或直接在 Android Studio 里打开 mobile/ 目录构建"
fi
"$GRADLE" --console=plain :app:assembleDebug "$@"

# 与 build_android.bat 保持同一出口：成品统一拷到仓库根的 dist/android/
SRC="$APK_DIR/debug/app-debug.apk"
[ -f "$SRC" ] || die "未产出 APK：$SRC"
mkdir -p "$ROOT/dist/android"
cp -f "$SRC" "$ROOT/dist/android/wb2api-tasks.apk"

echo
echo "完成："
echo "  dist/android/wb2api-tasks.apk"
echo "安装：adb install -r dist/android/wb2api-tasks.apk"

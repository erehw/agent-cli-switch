# 编译 acsw，并把版本号（git describe）注入进去。
# 用法：.\build.ps1                  # 输出 dist\acsw.exe
#       .\build.ps1 -Out D:\x\acsw.exe
param([string]$Out = "dist\acsw.exe")
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    $v = (git describe --tags --always --dirty 2>$null)
    if (-not $v) { $v = "dev" }
    go build -trimpath -ldflags "-X main.version=$v" -o $Out ./cmd/acsw
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & $Out version
} finally {
    Pop-Location
}
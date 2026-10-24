param([switch]$ExerciseLayouts)
$ErrorActionPreference = 'Stop'

# Discover only Glance's own loopback settings window. Never print its token.
$active = @()
$seen = @{}
Get-CimInstance Win32_Process -Filter "Name='msedge.exe'" | ForEach-Object {
    if ($_.CommandLine -match '--app="?(http://127\.0\.0\.1:\d+/\?t=[a-f0-9]+)') {
        $uri = [uri]$Matches[1]
        $base = $uri.GetLeftPart([UriPartial]::Authority)
        if (-not $seen.ContainsKey($base)) {
            $seen[$base] = $true
            $headers = @{'X-Glance-Token' = $uri.Query.Substring(3)}
            try {
                $meta = Invoke-RestMethod ($base + '/api/meta') -Headers $headers -TimeoutSec 2
                if ($meta.version -and $meta.catalog) { $active += @{Base=$base; Headers=$headers; Meta=$meta} }
            } catch { }
        }
    }
}
if ($active.Count -ne 1) { throw "需要恰好一个运行中的 Glance 设置页，当前找到 $($active.Count) 个。" }
$connection = $active[0]
function Invoke-Glance([string]$Path, [string]$Body = '') {
    $args = @{Uri=($connection.Base + $Path); Headers=$connection.Headers; TimeoutSec=10}
    if ($Body) { $args.Method='Post'; $args.ContentType='application/json'; $args.Body=$Body }
    Invoke-RestMethod @args
}

$config = Invoke-Glance '/api/config'
$original = $config | ConvertTo-Json -Depth 20
$results = @()
try {
    if ($ExerciseLayouts) {
        foreach ($mode in @('rows','line','list')) {
            $config.overlay.layout = $mode
            $config.overlay.graph = $true
            $applied = Invoke-Glance '/api/config' ($config | ConvertTo-Json -Depth 20)
            if ($applied.overlay.layout -ne $mode) { throw "布局 $mode 未应用" }
            Start-Sleep -Milliseconds 1200
            $state = Invoke-Glance '/api/state'
            $results += [PSCustomObject]@{
                Layout=$mode
                CPUClockMHz=$state.snapshot.values.'cpu.clock'
                FPSActive=$state.snapshot.fpsActive
                FPSProcess=$state.snapshot.fpsProcess
            }
        }
    }
} finally {
    if ($ExerciseLayouts) { $null = Invoke-Glance '/api/config' $original }
}
$state = Invoke-Glance '/api/state'
$finalConfig = Invoke-Glance '/api/config'
[PSCustomObject]@{
    Version=$connection.Meta.version
    ConfigVersion=$finalConfig.version
    LayoutRestored=($finalConfig.overlay.layout -eq ($original | ConvertFrom-Json).overlay.layout)
    Elevated=$state.snapshot.status.elevated
    PawnIO=$state.snapshot.status.pawnio
    NVML=$state.snapshot.status.nvml
    ETW=$state.snapshot.status.etw
    GPUPowerUnsupported=$state.snapshot.unsupported.'gpu.power'
    SelfCPUPercent=$state.snapshot.values.'self.cpu'
    PrivateMemoryMiB=$state.snapshot.values.'self.mem'
    FPSActive=$state.snapshot.fpsActive
    FPSProcess=$state.snapshot.fpsProcess
    ChatGPTExcluded=($finalConfig.general.fpsExclude -contains 'chatgpt.exe')
    LayoutChecks=$results
} | ConvertTo-Json -Depth 6

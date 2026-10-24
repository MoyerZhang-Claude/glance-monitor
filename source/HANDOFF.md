# Glance 交接说明（给接手的 AI / 开发者）

用户的电脑是机械革命耀世笔记本（Core Ultra 9 290HX Plus + RTX 5070 Ti Laptop，Win11 25H2），平时玩 2077、地平线、巫师 3、荒野大镖客 2。
用户的诉求：一个**简洁、不影响游戏帧率、可自由摆放**的专属监控悬浮窗，在游戏、桌面和托盘里都能看数据。
用户在线，会帮你测试（发截图、重启程序），可以直接请他操作。界面和沟通都用中文。

## 1. 现状（b8，已在用户电脑上验证）
- 程序：`D:\Claude\monitor\Glance.exe`；配置 `Glance.json`；日志 `glance.log`（都在 exe 同目录）
- 能用：悬浮窗、托盘数字图标 + 悬停卡片、设置页、CPU 占用/频率/温度/功耗、GPU 占用/频率/温度/热点/显存温度/显存、FPS + 1%/0.1% Low（2077 已实测）
- **没有在设备上验证过**：开机自启（计划任务 GlanceHUD）、一键更新流程、单行/列表布局的 GDI+ 渲染、帧时间曲线、“仅全屏时显示”、录屏隐藏、多显示器

## 2. 技术栈与编译
- 纯 Go（1.24，仅标准库 + syscall，无 cgo、无第三方依赖）。选 Go 是因为这台机器上没有 .NET SDK，Go 可以交叉编译出单个 exe。
- Windows 上编译：安装 Go（`winget install GoLang.Go`），然后在源码目录运行 `build.ps1`（或手动执行：
  `go test ./internal/...` 然后 `$env:GOOS="windows"; go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\Glance.exe .`）
- `rsrc_windows_amd64.syso` 是编译好的资源（图标、manifest：asInvoker + PerMonitorV2 DPI、版本信息），**不要删**。源文件在 `res/`（`app.rc`），需要 `llvm-windres` 才能重新生成。
- `go run ./cmd/webdemo` 可以在任何系统上用模拟数据打开设置页，方便调界面。

## 3. 部署 / 更新
- 程序运行时 exe 被锁住，不能覆盖。两种方式：
  1. 把新 exe 放成 `Glance.new.exe`，让用户右键托盘 → “安装新版本并重启”（或设置页顶部按钮）。程序会把自己改名为 `Glance.old.exe`、换上新版并自动重启（保持管理员权限）。
  2. 让用户先退出，再直接覆盖 `Glance.exe`。
- 程序默认会自动提权（UAC）；读 CPU 温度/功耗和 ETW 帧率都需要管理员权限。

## 4. 代码结构
- `main.go`：单实例互斥锁、提权重启、更新交接（`--updated` 会等旧进程退出）
- `internal/w32`：手写的 Win32 绑定（启动时 `SetDefaultDllDirectories` 限定只从 System32 加载 DLL）
- `internal/gdip`：GDI+ flat API 封装（float 参数直接走 syscall，Go 运行时会同时装入 XMM 寄存器，可以正常工作）
- `internal/sensors`：独立 OS 线程采样
  - `cpu.go`：PDH（Processor Utility、各核心 % Performance × Frequency）；Intel MSR 通过 PawnIO 读取（0x1A2 TjMax、0x1B1 封装温度、0x19C 各核心温度，读各核心时切换线程亲和性、0x606/0x611 RAPL 功耗）；AMD Zen 走 SMN
  - `pawnio.go`：PawnIO 的 IOCTL；签名模块（LGPL，来自 LHM 0.2.11）嵌入在 `pawnio/`
  - `gpu.go`/`nvapi.go`：NVML、PawnIO Nvidia 热点/显存温度；用 SetupAPI 查独显电源状态，休眠时不唤醒
  - `manager.go`：生成快照，日志里定期写 `sample #N` 和 `cadence` 行
- `internal/fps`：ETW 实时会话 `GlanceHUD-PresentTrace`（DXGI Present_Start/MPO、D3D9、DxgKrnl PresentHistory_Start，带事件 ID 过滤，NO_PER_PROCESSOR_BUFFERING，原始 QPC 时间戳）；`tracker.go` 计算 FPS 和 Low，有单元测试；`target_windows.go` 选择统计哪个进程（前台程序、排除名单、阈值、粘滞保持）
- `internal/layout`：快照 + 配置 → 悬浮窗行和卡片区块（与平台无关，有测试；`assets/app.js` 里有一份 JS 镜像，改动要两边同步）
- `internal/ui`：主隐藏窗口、悬浮窗（UpdateLayeredWindow，编辑模式用 HTCAPTION 拖动并吸附）、托盘（v4，Win11 不发 NIN_POPUPOPEN，所以用鼠标移动 + 轮询判断悬停）、卡片、菜单、热键、设置页后端
- `internal/web`：127.0.0.1 随机端口 + 令牌请求头 + Host 检查；以非管理员身份打开 Edge `--app` 窗口（复用 explorer 的令牌）

## 5. 已确认的结论（不要重复踩坑）
- **GPU 功耗**：在这块显卡上，NVML 的 PowerUsage、字段 185/186、TotalEnergy、所有功耗墙接口都返回 3（NOT_SUPPORTED），NVAPI ClientPowerTopology 条目数为 0。不过“机械革命控制中心”（`C:\Program Files\OEM\机械革命控制中心`）能显示显卡功耗 → **这是下一个任务**：搞清楚它从哪里读的（可能是 .NET 程序，可以用 ILSpy 反编译；也可能是 WMI/EC/厂商驱动）。EC 只能读，**绝对不要写 EC**。Uniwill 系的 EC 资料（WMI `AcpiTest_MULong.GetSetULong`）里只公开了温度/风扇/电池寄存器。
- **供电温度（VRM）**：笔记本读不到，已标记为不支持并隐藏（如果 EC 里有，可以再加）。
- **帧率**：开 DLSS 帧生成时，2077 的 DXGI present 间隔是均匀的，生成帧也算在内，FPS 和显示画面一致。1% Low 之前不稳定，是因为游戏会在 150 帧（游戏中）和 60 帧（切出 / 菜单）之间切换；现在的做法是**按场景**统计（以 1 秒为一段、帧时间中位数变化超过 35% 就重新开始），用户确认稳定了。帧生成“突发帧”平摊的逻辑保留着，但这台机器上没有触发。
- 这台机器的独显一直处于 D0（大概率是独显直连模式）。`msedgewebview2.exe` 和 `claude.exe` 会持续以 240 FPS 刷新 → 已加入默认的不统计名单。
- 机器上装有 ACE（腾讯反作弊）：**不要往游戏里注入任何东西**，保持外部透明窗口的方案。

## 6. 下一步建议
1. 从机械革命控制中心那里拿到 GPU 功耗（可能还有风扇转速、供电温度）
2. 在设备上验证开机自启和一键更新
3. 让用户试单行/列表布局、帧时间曲线、“仅全屏时”，必要时调整 GDI+ 渲染
4. 每次改完都要跑 `go test ./internal/...`，并且 `GOOS=windows go vet -unsafeptr=false ./...` 要通过

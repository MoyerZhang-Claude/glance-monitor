# Glance 交接说明（给接手的 AI / 开发者）

用户的电脑是机械革命耀世笔记本（Core Ultra 9 290HX Plus + RTX 5070 Ti Laptop，Win11 25H2），平时玩 2077、地平线、巫师 3、荒野大镖客 2。
用户的诉求：一个**简洁、不影响游戏帧率、可自由摆放**的专属监控悬浮窗，在游戏、桌面和托盘里都能看数据。
用户在线，会帮你测试（发截图、重启程序），可以直接请他操作。界面和沟通都用中文。

## 0. b10（2026-10-19，Claude 收尾优化）
- GPU 功耗自动恢复：启动时探测失败（方法为 none）后，每 60 秒安静地重试 NVML PowerUsage / 字段 186、185 / 能耗计数器；成功后日志记 `GPU power recovered: method X`，不用重启 Glance。
- CPU 频率防尖峰：PDH “% Processor Performance” 采样窗口短于 0.45 秒时（启动、设置变更触发的提前采样），沿用上一个有效值。b9 日志里启动第二个样本出现过 5874MHz。
- 周期日志里新增 `self: private / goHeap / goSys`，可以区分 Go 堆和原生库（GDI+、NVML、PDH）的内存占用。设置页自报私有内存约 74MiB，还没分析具体构成。
- 版本号为 `1.0.0-b10`，用容器里的 Go 1.24.7 交叉编译。

## 1. 现状（b9，2026-10-19 已部署并验证）
- 程序：`D:\Claude\monitor\Glance.exe`；配置 `Glance.json`；日志 `glance.log`（都在 exe 同目录）
- 能用：悬浮窗、托盘数字图标 + 悬停卡片、设置页、CPU 占用/频率/温度/功耗、GPU 占用/频率/温度/热点/显存温度/显存/**功耗（用户重启后 NVML 恢复，已确认）**、FPS + 1%/0.1% Low（2077 已实测）
- 当前显示版本 `1.0.0-b9`，配置版本 3，实际运行程序与 `source/dist/Glance.exe` SHA256 相同：`9325181FEF41BBF09A6787AE5116DAE5F93F0FD34D2F2B32288A7E73C80F24BA`。
- 已实测：b8 → b9、b9 初版 → b9 最终版的本地更新和自动重启，重启后仍为管理员，PawnIO/NVML/ETW 均为 `ok`。
- 最后一次只读复核时用户已启动 2077：b9 正确选择 `Cyberpunk2077.exe`，同时排除持续刷新的 ChatGPT/控制中心；这不等于新版本游戏性能基准测试。
- 已实测：运行中的 rows/line/list 切换和原配置恢复；同一套原生 GDI+ 渲染器输出三种布局及帧时间曲线（模拟数据），100% / 150% 缩放，图像已检查无明显裁切。
- **仍未完成设备验收**：开机自启（目前未启用，不能把只读检查当作登录启动验证）、“仅全屏时显示”、录屏隐藏、多显示器、真实游戏中的帧时间曲线。不要宣称这些已通过。
- 本轮备份：`D:\Claude\monitor\backups\b8\Glance.exe` 和 `Glance.json`；当前 `Glance.old.exe` 为上一轮 b9 构建。

## 2. 技术栈与编译
- 纯 Go（1.24，仅标准库 + syscall，无 cgo、无第三方依赖）。选 Go 是因为这台机器上没有 .NET SDK，Go 可以交叉编译出单个 exe。
- 源码已经从原始 zip 恢复到本目录。已准备便携 Go 1.27.2：`D:\Claude\monitor\tools\go\bin\go.exe`，从 Go 官方下载并核验 SHA256。项目仍声明 Go 1.24 最低版本，无第三方运行依赖。
- Windows 上编译：在源码目录运行 `build.ps1`，会优先使用 PATH 中的 Go，没有则使用 `../tools/go`。任一步测试、vet 或编译失败均立即终止。（或手动执行：
  `go test ./internal/...` 然后 `$env:GOOS="windows"; go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\Glance.exe .`）
- `rsrc_windows_amd64.syso` 是编译好的资源（图标、manifest：asInvoker + PerMonitorV2 DPI、版本信息），**不要删**。源文件在 `res/`（`app.rc`），需要 `llvm-windres` 才能重新生成。
- `go run ./cmd/webdemo` 可以在任何系统上用模拟数据打开设置页，方便调界面。

## 3. 部署 / 更新
- 程序运行时 exe 被锁住，不能覆盖。两种方式：
  1. 把新 exe 放成 `Glance.new.exe`，右键托盘 → “安装新版本并重启”（或设置页顶部按钮）。程序会把自己改名为 `Glance.old.exe`、换上新版并自动重启（保持管理员权限）。b9 校验 PE 架构、节完整性和 Go 主模块 `glance`；不完整文件不删除，启动失败回退原文件名。备份保留到下一次更新。此校验用于兼容性/损坏检查，不是数字签名认证。
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
- **GPU 功耗（最新结论：可用）**：用户重启后已正常显示 GPU 瓦数，悬浮窗/托盘截图分别为 19W、26W。`2026/10/19 07:12:37` 日志明确记录 `NVML power usage: ret=0 value=15907mW`、`GPU power method: nvml`。之后桌面样本约 8–34W，2077/高负载样本约 124–138W，随负载变化。原来 NOT_SUPPORTED 是重启前的状态，**不能再据此认定这块显卡不支持功耗读取**。具体为何重启恢复尚未确定，不要推断成某个确定的驱动故障。
- 历史调查：重启前 NVML PowerUsage、字段 185/186、TotalEnergy、功耗墙接口均返回 3，GPU 98% 负载时也一样；NVAPI ClientPowerTopology 条目数为 0。控制中心旧截图没有 GPU W，AIDA64 旧截图只有 GPU TDP% 0%。这些是当时的证据，不代表重启后的状态。
- 已静态检查厂商 `GCUService.exe`，GPU 监控引用其内嵌 LibreHardwareMonitorLib 0.9.6；该库普通 NVIDIA 瓦数仍走 NVML，NVAPI 结构和 Glance 相同，没有找到可接入的新接口。服务代码被混淆，不能把库引用当成完整调用链证明。控制中心雷达“功耗”来自按模式选择的预设评分，不是实时传感器。详见 `GPU-POWER-INVESTIGATION.md`。**不要继续基于旧前提反复逆向控制中心，不要用 TGP 或百分比捏造瓦数。**
- EC 只能读，**绝对不要写 EC**。本轮没有访问 EC，也没有运行厂商调节方法。
- **供电温度（VRM）**：笔记本读不到，已标记为不支持并隐藏（如果 EC 里有，可以再加）。
- **帧率**：开 DLSS 帧生成时，2077 的 DXGI present 间隔是均匀的，生成帧也算在内，FPS 和显示画面一致。1% Low 之前不稳定，是因为游戏会在 150 帧（游戏中）和 60 帧（切出 / 菜单）之间切换；现在的做法是**按场景**统计（以 1 秒为一段、帧时间中位数变化超过 35% 就重新开始），用户确认稳定了。帧生成“突发帧”平摊的逻辑保留着，但这台机器上没有触发。
- 这台机器的独显一直处于 D0（大概率是独显直连模式）。`msedgewebview2.exe` 和 `claude.exe` 会持续以 240 FPS 刷新 → 已加入默认的不统计名单。b9 又根据日志加入 `chatgpt.exe`、`codex.exe`、`ccuwinui.exe`，配置 v2 → v3 自动追加并保留自定义项；升级后用户仍可删除条目。进程名未识别前不再作为 FPS 目标，避免新启动设置浏览器在名称缓存刷新前短暂漏过排除名单。
- b9 将硬件采样间隔下限统一为 250ms（含设置变化和启动后的首个周期）。此前日志实见两次采样间隔 4ms、PDH 频率误报 42416MHz。修复后布局切换检查读数为 4.16–5.18GHz，未再重现。
- 机器上装有 ACE（腾讯反作弊）：**不要往游戏里注入任何东西**，保持外部透明窗口的方案。

## 6. 下一步建议
1. GPU 功耗已经由现有 NVML 路径正常读取，无需另接厂商接口。b10 已加入启动探测失败后的低频重试（每 60 秒）；如果再次消失，看日志里有没有 `GPU power recovered`。
2. 用户需要开机自启时，验证任务创建、手动运行、实际注销/登录；当前尚未启用，不要自行重启电脑。
3. 继续真实游戏下的帧时间曲线、“仅全屏时”、录屏隐藏和多显示器验收。三种布局及原生 GDI+ 模拟曲线已有基础验收。
4. 每次改完都要跑 `go test ./internal/...`，并且 `GOOS=windows go vet -unsafeptr=false ./...` 要通过

## 7. 本轮验证工具与记录
- `tools/verify-live.ps1`：只读核验运行中的设置服务；加 `-ExerciseLayouts` 会短暂切换三种布局/开启曲线，并在 finally 恢复原配置。不要输出 URL 的令牌。测试时需要打开 Glance 设置页。
- 原生渲染验收：设置 `$env:GLANCE_RENDER_DIR='D:\Claude\monitor\investigation\renders'` 后运行 `go test ./internal/ui -run TestNativeOverlayRendering -v`。输出是模拟数据，不是游戏实测帧率。
- `internal/sys/update_test.go`：真实 Go/PE 样本、错误架构、DLL、损坏/截断、非 Glance 程序、更新成功保留备份、启动失败回退、相同版本/无效文件保护。
- 原生 PNG 与运行态核验：`D:\Claude\monitor\investigation\renders`、`b9-live-verification.json`；当次设置页打开状态自报 CPU 0.06%、私有内存约 74MiB，只是一时采样，不等价于游戏帧率影响评测。
- 本轮只使用外部窗口/只读 API，没有游戏注入，没有 EC 写入。
- 重启前用户运行 2077 时独立 NVML 复测：GPU 占用 98%，`nvmlDeviceGetPowerUsage` 仍返回 3；对应旧状态记录见 `investigation/b9-game-verification.json`。**此记录早于 07:12 重启后的恢复，不能当成当前状态。**

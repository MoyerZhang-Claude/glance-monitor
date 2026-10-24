# GPU 功耗调查记录（2026-10-19）

最新结论：**用户重启后 GPU 瓦数已由现有 NVML 路径正常读取，无需新接口或代码修改。** 原先“不支持”的判断仅描述重启前的运行状态，不能作为硬件永久不支持的结论。

## 重启后的确认

- 用户提供 Glance 悬浮窗/托盘卡片截图，GPU 分别显示 19W、26W。
- `glance.log` 在 `2026/10/19 07:12:37` 记录 `NVML power usage: ret=0 value=15907mW` 和 `GPU power method: nvml`，说明来源是实时 NVML 读数。
- 后续桌面样本约 8–34W，2077/高负载样本约 124–138W，随负载变化，确认不只是固定功耗上限。
- 重启恢复的具体原因尚未确定。NVAPI 仍无功耗拓扑条目不影响 NVML 正常工作。

## 重启前的历史证据

- GPU：RTX 5070 Ti Laptop GPU，PCI bus 2、slot 0。
- NVML 原有探测全部返回 NOT_SUPPORTED；本轮又在独立 Python/ctypes 进程直接查询 System32 的 nvml.dll，设备名称一致，`nvmlDeviceGetPowerUsage` 三次均返回 3。b9 重启日志同样返回 3。
- 用户随后启动 2077，独立 NVML 查询同时获得 GPU 占用 98% 和功耗返回码 3，说明此次失败也存在于高负载状态。
- NVAPI ClientPowerTopology 返回成功但条目数为 0。Glance 使用的 72 字节结构与控制中心内嵌 LHM 定义一致。
- 用户提供的机械革命控制中心主页截图：显卡区 2%、585MHz、810MHz、46°C、12226MB，没有 W。此前交接关于“控制中心能显示 GPU 功耗”的描述错误，用户本轮已更正。
- 用户提供的 AIDA64 Business 8.30.8300 截图：CPU 封装 12.75W、CPU IA 核心 4.38W，GPU 仅 `GPU TDP% 0%`。这张图不能证明该机已经有实时 GPU 瓦数。

## 控制中心静态分析

安装目录：`C:\Program Files\OEM\机械革命控制中心`。

1. `AiStoneService/MyControlCenter/GCUService.exe` 的 `GCUService.MySystem.GPUInfo.GpuMonitor` 引用 `LibreHardwareMonitor.Hardware.Computer`，包含 `GetGpuPower`。主体经混淆，方法内容不可直接恢复，因此不能证明所有运行路径。
2. 从 Costura 资源中解出 LibreHardwareMonitorLib，版本 `0.9.6+3d331e3370efb858411f19511373eff65a218701`。
3. 该库的 `NvidiaGpu` 普通 GPU Package 功耗走 `NvidiaML.NvmlDeviceGetPowerUsage`，除以 1000 得 W；NVAPI PowerTopology 作为 Load/% 指标。另有 ASUS Astral 特有传感器，不适用于此笔记本。
4. 原生 `GPUInfoDLL.dll` 导出频率、温度、显存、利用率等接口，没有 GPU 功耗导出。
5. 从安装包只读提取 `CCUWinUI.dll`，`HomePageViewModel` 中显卡栏没有瓦数绑定。`UpdateRadarDatas` 按 Office/Balance/Turbo 模式调用 `AppSetting.GetRadarData`，雷达“功耗”来自预设评分。

分析证据保存在工作区 `investigation/`，厂商二进制和反编译文件不包含在源码交付包中。没有修改厂商文件，没有调用超频/风扇设置，没有 EC 读取或写入，没有游戏注入。

上游实现参考：[LibreHardwareMonitor NVIDIA GPU 代码](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/blob/master/LibreHardwareMonitorLib/Hardware/Gpu/NvidiaGpu.cs)。本轮判断以安装文件和用户截图为准，不依赖上游版本完全一致。

## 后续处理

保留当前 NVML 实现。若再次出现启动探测失败但稍后驱动恢复的情况，先记录复现条件；目前 Glance 只在 NVML 初始化时选择功耗来源，可考虑按低频重试改善自动恢复，不需要扫描 EC 或依赖厂商私有接口。不要把未知值显示为 0W，也不要把 TDP 百分比或功耗上限当成实时瓦数。

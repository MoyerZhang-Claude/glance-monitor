# Glance 硬件监控悬浮窗

Windows 桌面 / 游戏内的半透明悬浮窗，实时显示 CPU、GPU 的温度、占用、功耗和游戏帧率。单文件、无需安装，CPU 占用接近 0，不注入游戏进程。

## 下载

到 [Releases 发布页](https://github.com/MoyerZhang-Claude/glance-monitor/releases/latest) 下载 `Glance-x.x.x-windows.zip`，解压后双击 `Glance.exe`（Windows 10/11）。

- 读取硬件传感器需要管理员权限，首次启动会弹出确认，点“是”。
- 程序没有代码签名，Windows 若提示“已保护你的电脑”，点“更多信息 → 仍要运行”。

快捷键、开机自启、常见问题见 [使用说明.txt](使用说明.txt)。

## 从源码构建

源码在 [source-code/](source-code/)，需要 Go 1.24+：

```powershell
cd source-code
.\build.ps1
```

生成的程序在 `source-code\dist\Glance.exe`。开发记录见 [source-code/HANDOFF.md](source-code/HANDOFF.md)。

## 第三方组件

- CPU 传感器通过 PawnIO 驱动读取（模块来自 namazso/PawnIO.Modules，LGPL-2.1）
- 帧率统计的事件定义参考了 Intel PresentMon（MIT）

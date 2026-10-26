# 文件与工作区

默认工作目录是 `<root>/work/<bot>/`，同一安装的共享目录是 `<root>/work/shared/`；`<root>` 默认 `~/.xbot`，可由 `--root DIR` 指定。Bot 数据目录 `<root>/bots/<bot>/` 与工作目录不同，别把记忆或 transcript 的位置猜成工作目录。

有 tools 能力时用 `read_file`、`write_file`、`edit_file` 及本机已安装的解释器工具（`zsh`、`bash`、`sh`、`powershell`、`cmd`、`git-bash`，按平台提供）操作文件。解释器说明包含名称和版本，调用必须用 `description` 说明命令目的。相对路径按当前工作目录解析；主线 shell 的目录可持续保存到 `bots/<bot>/cwd`，子代理继承启动时目录，之后各自独立。当前路径以环境 paths、工具回执的 cwd 为准，手册不会查询实例磁盘。

主人可以用文件管理器或编辑器打开文件；让 Bot 告诉你实际产物路径前，应该先核实产物确实存在。工作区中的 AGENTS.md（没有时用 CLAUDE.md）提供项目约定，不改变内核权限表、密钥保护或主人授权。普通文件写入不代表处处沙箱隔离；记忆文件仍有专门的格式、大小与追加校验。

# 开发与维护

本文记录本地开发、架构与权限、FPK 打包、GitHub Release、第三方源发布及故障排查。应用功能和安装说明见 [项目首页](../README.md)。

## 本地运行

需要 Linux、Go 1.26+、Node.js 22.12+、npm、Python 3。开发模式拒绝 root、非回环监听地址、非回环 Host 和其他用户身份。

```bash
./scripts/build.sh amd64
./build/fn-terminal-linux-amd64 dev
```

打开 <http://127.0.0.1:8088/app/fnterminal/>。页面自动以运行开发服务的当前账号进入真实 shell；不是飞牛登录态模拟器，也不能用它测试跨用户身份切换。

开发界面热更新时，先运行上述开发服务，再执行 `npm --prefix web run dev`，打开 Vite 输出的本机地址下 `/app/fnterminal/` 路径。

界面支持终端尺寸同步、选区复制、剪贴板粘贴、清屏、全屏、手动重新连接、多标签与递归分屏。浏览器剪贴板按钮需要 HTTPS 或可信本机环境；终端标准键盘粘贴仍可使用。

## 架构与权限

```text
飞牛桌面 / 浏览器
  → 飞牛统一网关（登录态认证、用户 Header）
  → root 持有的 app.sock（监听 FD 传给包用户 Web 进程）
  → 非 root Web 服务（身份、Origin、协议校验）
  → broker.sock（SO_PEERCRED 验证包用户）
  → root PTY 辅助服务（账号再校验、会话限额）
  → 设置用户 UID / GID / 附加组后启动登录 shell
```

生产服务不监听独立 TCP 端口。Web 进程不以 root 运行；特权辅助服务只接受固定的会话创建、字节输入、尺寸变化与连接结束，不接受任意启动命令、可执行路径或环境变量。

目标用户必须 UID ≥ 1000，且不是 root 或应用包用户；用户名必须与系统 UID 映射一致，shell 必须为允许的 bash/sh/zsh/fish 路径。应用不会替用户开启 SSH、改变 shell、增加 sudo 权限或保存账号密码。普通用户的账号若配置为 nologin，则显示拒绝访问，不擅自改动系统策略。

生命周期脚本在执行应用二进制、打开运行日志或 PID 文件前，检查可信的回调位置及共享父目录。飞牛可能把解包后的程序设置为 0775；安装/升级会仅锁定应用自己的目录，再把整个固定 payload 复制到新的 root 私有临时文件，按可信回调中嵌入的 SHA-256、长度和文件路径校验。全部文件通过后才替换发布，程序为 root:root 0755，模板与 UI 文件为 0644；程序内部还会再次检查运行路径。旧文件的可写 FD 只能修改退役 inode。

SHA-256 清单在打包时生成，最终 FPK 检查器会核对清单与实际 app.tgz，包括 fnpack 自动加入的权限/资源声明。篡改内容、符号链接、世界可写目录或不可信共享父目录仍被拒绝。不会只对不可信程序 chown/chmod 后直接执行，也不会修改存储卷的公共父目录权限。信任起点仍是飞牛包管理器保护的生命周期调度与回调脚本位置。

默认网关 Socket 权限为 root:root 0600；需要在真机确认飞牛网关具有访问能力。若系统网关进程使用其他账号，必须明确验证其身份后适配，不能用放宽至所有用户可写的方式解决。

Web 服务是认证信任链的一部分：攻破它仍可能滥用辅助服务访问允许范围内的用户账号。进程拆分并不等于消除此风险。

## 会话行为

- 默认每用户最多 4 条、应用总计 32 条会话，限制在辅助服务执行。
- 15 分钟无实际终端输入输出，或达到总时长 1 小时后关闭。心跳和窗口尺寸变化不刷新闲置计时。到达上限时先在终端内显示断开原因，再结束会话；服务日志记录 `terminal expired` 及 `idle_timeout` / `max_duration`。
- Web 服务每 25 秒发送一次 WebSocket Ping，让空闲连接不被网关按无流量断开；连续 3 次（每次等待 10 秒）收不到 Pong 视为对端失联，结束会话并释放配额。
- 每条连接绑定创建时的 UID，客户端无法通过消息或 URL 参数切换用户。
- 浏览器关闭、网络断开或重连时结束原会话和同一 POSIX session 中的作业进程组；shell 自行退出时也会在短暂输出排空后关闭会话并释放配额。不会自动恢复旧 shell，重连明确创建新会话。主动通过 setsid 脱离会话的守护进程不属于这一清理保证。
- 输出使用二进制字节帧，支持跨读取边界的 UTF-8；浏览器每渲染一个数据块发送 ACK，最多保留 8 个未确认块，超时断开慢客户端。
- 仅记录 UID、会话起止及错误，不记录命令、终端输出或凭据。终端内程序自身可能产生 shell history，这是账号原有的系统行为。
- 统一网关在连接建立时认证。尚未核实官方会话撤销接口，**NAS 注销后不会保证立即切断已建立的终端连接**；新连接仍需认证，已有连接受时长限制约束。

## 配置

示例见 [configs/config.example.json](../configs/config.example.json)。生产配置位于 `${TRIM_PKGETC}/config.json`，必须由 root 持有且包用户不可写；可设 root:包用户组 0640。

`enabled` 默认 false，通过安装/配置向导明确开启。`allowed_users` 为空时允许所有符合账号策略且能经系统网关访问本应用的用户；设置用户名数组可进一步收窄。系统侧的应用授权配置也需要在真机验证。

默认自动适配局域网 IP、IPv6 和 FN ID 访问，无需每个用户填写地址。页面通过同源请求领取 60 秒内有效的一次性连接凭据，绑定当前网关账号及浏览器 Origin；WebSocket 校验并消费凭据后才创建终端，不依赖网关转发的 Host。票据不写入 URL、持久存储或日志；凭据接口不授权跨域读取，跨站预检被拒绝。

`allowed_origins` 为空表示自动适配；管理员仍可把它设置为完整 Origin 列表作为额外限制，不支持通配符。升级保留已有管理员策略。如果曾为 0.1.4 手动加入临时来源白名单，可在应用设置中勾选“恢复自动适配访问地址”，同时保持“启用终端访问”，保存一次即可清除地址限制，无需逐个用户修改。保存设置会重启服务并结束旧会话。

## 仓库结构

根目录采用飞牛应用包结构，源码与打包元数据在同一仓库维护：

```text
manifest              # 手写静态 x86 应用信息
manifest.arm64        # 手写静态 ARM 应用信息
ICON.PNG / ICON_256.PNG
app/ui/               # 桌面入口和图标
cmd/                  # 飞牛生命周期回调
config/               # 权限与资源声明
wizard/               # 安装、设置向导
backend/cmd/fn-terminal/  # Go 程序入口，与生命周期 cmd 分开
internal/             # Go 服务实现
web/                  # 前端源码
configs/              # 程序配置模板
scripts/              # 构建、检查、源索引及模板工具
.github/workflows/release.yml
```

Go 源码不会随生命周期 cmd 打入安装包。编译后二进制及配置模板会放入临时打包目录，可信 payload 校验清单也在那里生成；请使用下述打包脚本，避免绕过这个步骤直接对源码根目录调用 fnpack。

## FPK 打包

官方工具：[fnpack 文档](https://developer.fnnas.com/docs/cli/fnpack/)。先安装工具并加入 PATH，然后构建对应架构：

```bash
./scripts/build.sh amd64
```

直接生成 release 包：

```bash
./scripts/package.sh amd64
```

应用信息直接维护在根目录静态 [manifest](../manifest)，打包过程原样复制，不再生成或通过环境变量覆盖它。修改名称、版本、维护者、联系方式、兼容版本或更新说明时，直接编辑该文件。根 manifest 的架构是 `x86`；ARM 使用独立静态 [manifest.arm64](../manifest.arm64)，除 `platform=arm` 外其余字段必须一致，脚本会拒绝不一致的 ARM 构建。不要把包含单架构二进制的包声明为 `platform=all`。

当前版本 `0.1.13`，产物为 `build/fnterminal-0.1.13-amd64.fpk`。构建脚本从静态 manifest 读取版本并写入 Go 二进制，打包文件名也读取该文件；后续发版不需要改脚本中的版本常量。`maintainer` / `distributor` 为 `medivh-jay`，开发者与发布者链接均指向 [项目仓库](https://github.com/medivh-jay/fn-terminal)。最低版本仍应按实际兼容性填写，未声明不表示兼容所有 fnOS。

脚本会创建独立的 `build/fpk-amd64.*/` 打包目录，检查 ELF 架构、向导、生命周期、图标和默认禁用状态，再调用官方 fnpack。非 root 构建使用 util-linux 的 `unshare --user --map-root-user`，仅在新用户命名空间内把构建者映射为 root，以生成正确的 root 归属元数据；不需要构建机 sudo 权限。最终检查覆盖外层与 app.tgz 内层 UID/GID、生命周期权限和官方 payload checksum，避免把构建机的 UID 1000 带进特权安装包。

工具不在 PATH 时，可通过 `FNTERMINAL_FNPACK` 指定官方 fnpack 的完整路径。没有官方工具时可设 `FNTERMINAL_STAGE_ONLY=1` 只准备并检查目录；这不表示已生成 FPK。

支持 `arm64` 交叉编译及 `platform=arm` 打包；没有 ARM 真机验收记录前，不将它视为已验证平台。二进制嵌入前端资源，不依赖 NAS 上安装 Node.js 或 Go。

`config/privilege` 声明 Root 生命周期，目的是运行受限监督/辅助进程并降权 Web 服务。需要向应用中心说明这项权限的用途和边界；是否允许上架以实际审核为准。

安装默认不启用终端；启用只来自管理员向导。升级保留已有配置。停止/升级/卸载先结束会话；卸载仅清理应用自己的运行文件，不改动用户主目录或用户通过 shell 创建的文件。

## GitHub Release 与应用坏心源

[应用坏心当前实现](https://github.com/RROrg/fn-apps/blob/main/fn-appdownload/app/server/appdownload.py#L1021)读取配置的 JSON 地址，不会从 GitHub 仓库首页发现 manifest 或 Release。发布后请在添加源中使用：

```text
https://github.com/medivh-jay/fn-terminal/releases/latest/download/fnpack.json
```

GitHub 仓库需公开且已发布至少一个 Release。`fnpack.json` 是 Release 附件，无需在主分支提交带占位下载地址的源文件；也可以使用自行托管的该 JSON 直链。默认索引采用以应用 ID 为顶层键、以 `arch_diff` 区分架构的 FnDepot 格式，包含真实 FPK 的版本、架构、SHA-256 与大小；下载链接固定到对应版本标签，后续发版只更新稳定入口指向的新索引。其他商店的仓库首页识别规则各有差异，本仓库不宣称能用任意商店的仓库模式直接添加。

默认格式与常见第三方仓库一致：`fnterminal` 下提供 `display_name`、`platform`、`desc`、`version`、`author`、`author_url`、`bug_report_url`、`install_type`、`size` 和 `arch_diff`，每个架构有自己的下载地址及实际大小。`labels` 使用逗号分隔文本，以兼容应用坏心当前将标签转为字符串的实现。URL 字段使用普通 URL 字符串。

同时发布 V2 索引，支持 V2 的客户端可改用 `releases/latest/download/fnpack.v2.json`，其中按架构提供整数大小与 SHA-256 供客户端校验。旧格式中的 SHA-256 扩展字段不代表所有旧客户端会执行校验，手动校验可使用 `SHA256SUMS`。本地生成 V2 时增加 `--format v2` 即可。

桌面入口类型仍为 `app/ui/config` 中的 `type=iframe`，它控制飞牛桌面如何打开应用，与应用源 JSON 的结构分开维护。

上传代码后，日常发版流程：

1. 编辑两份静态 manifest 的版本与更新说明，保持公共字段一致。
2. 提交代码，推送与 manifest 相符的 `v版本号` 标签，例如 `v0.1.13`。
3. [Release 工作流](../.github/workflows/release.yml)读取当前 GitHub 仓库名，执行构建与检查，生成 x86 / ARM FPK、`fnpack.json`、`fnpack.v2.json` 和 `SHA256SUMS`，上传完整附件后发布 Release。
4. 在应用坏心添加上述源地址，刷新即可发现应用。升级只需继续推送新版本标签。

手动运行 Actions 并选择普通分支时只构建、不发布。自动发版需要仓库允许 Actions，并允许该工作流的发布任务使用 `contents: write` 创建 Release；无需额外填写个人访问令牌。工作流不会自动提交或修改静态 manifest。ARM 目前为交叉编译与归档检查，不等于 ARM 真机验收。

也可以本地生成源索引，再自行上传对应 Release（此命令仅写本地文件）：

```bash
./scripts/build.sh amd64
./scripts/package.sh amd64
./scripts/build.sh arm64
./scripts/package.sh arm64
python3 scripts/build_source.py \
  --repository medivh-jay/fn-terminal \
  --tag v0.1.13 \
  --output build/fnpack.json \
  build/fnterminal-0.1.13-amd64.fpk build/fnterminal-0.1.13-arm64.fpk
```

Release 附件名要与本地 FPK 文件名一致。版本标签必须含根目录 `ICON_256.PNG`，源使用对应标签的图标直链。仓库推送、GitHub Actions 云端运行和实际商店安装尚需发布后验收。

## 检查与测试

```bash
go test -race ./...
go vet ./...
npm --prefix web ci
npm --prefix web test
npm --prefix web run build
```

源码格式化使用固定版本 Prettier 3.9.10、Black 26.10.1、shfmt 3.14.1 和 Go 自带的 gofmt；需要 `rg`、`uv`、Go 及已安装的前端依赖。首次运行会下载格式化工具，生成的静态资源和历史打包目录不参与源码格式化：

```bash
npm --prefix web run format
npm --prefix web run format:check
```

格式化后会从模板重新生成生命周期回调中的可信前置检查。请编辑模板后运行格式化，不要单独修改生成片段。

真实浏览器测试需要先构建 amd64 开发程序：

```bash
cd web
npx playwright install chromium
npx playwright test
```

浏览器测试会启动仅回环监听的独立开发服务和 Host 改写代理，验证真实命令输出、中文、标签隔离、后台输出 ACK、满额重连、异步粘贴隔离、清屏、窄屏单行布局及认证/跨站拒绝。测试仅在当前用户的 HOME 中运行 `printf`、短暂 `sleep` 和 Shell 变量赋值，不修改文件。

界面使用 30px 顶部标签栏，外框贴边且无 footer，终端内容左右各留8px、顶部留8px。点击 + 新建独立 Shell，切换保留输出，× 结束标签内全部会话；使用 × 关闭最后一个标签后可再次新建。工具栏作用于当前焦点窗格，异步剪贴板读取仍固定原窗格和连接，期间重连则取消旧粘贴。标签支持左右方向键、Home/End 切换和 Delete 关闭。所有标签及拆分窗格共用管理员配置的每用户会话上限（默认4），多个应用窗口也共享服务端配额。

终端区域右键菜单提供复制、粘贴、拆分视图、关闭。指向“拆分视图”后在右侧展开“上下、左右”，新窗格仍可继续拆分；每个窗格为独立 Shell，已有会话和输出保留。关闭窗格后相邻布局自动合并，最后一个窗格关闭对应标签。分隔条可拖动或用方向键调整，并同步实际PTY尺寸。菜单支持Shift+F10、菜单键、方向键、Enter及Escape，右键事件不会交给终端程序。浏览器剪贴板权限限制仍按原有提示处理。

分屏快捷键为 `Ctrl+(`（左右）和 `Ctrl+)`（上下）；常见键盘对应 `Ctrl+Shift+9`、`Ctrl+Shift+0`，作用于当前焦点窗格，仍受会话上限限制。Shell 执行 `exit` 或正常结束时关闭对应窗格，最后一个窗格退出后通过飞牛官方 SDK 关闭当前应用页面；网络断开、超时仍保留窗格供重新连接。关闭应用页面不停止 NAS 服务或其他窗口。[飞牛关闭接口](https://developer.fnnas.com/api/page/ui/)要求 fnOS 1.2.0401+（手机端 App 1.34.0+）；普通浏览器中脚本打开的窗口使用 `window.close()`，手动打开的标签若被浏览器阻止关闭，会显示退出提示。

右上角主题选择提供松绿、石墨、午夜蓝、浅色，同步改变界面和所有终端标签的完整 ANSI 配色，保留现有 Shell 和输出。偏好保存在当前浏览器的该访问来源中（局域网和 FN ID 地址各自保存），同源应用窗口同步；浏览器禁止存储时仍可切换，刷新后使用默认松绿。不会修改 NAS 系统主题或服务配置。

右侧滚动条使用随主题变化的细圆角滑块，悬停和拖动时加深显示；保留完整拖动区域，支持滚轮与历史拖动。浏览器原生回退采用细滚动条，支持的浏览器使用圆角样式。

真机兼容性与应用中心发布需要另外验收。应用中心提交前还需完成真实维护者资料、项目授权许可选择、权限方案审核、系统/硬件兼容矩阵和安装/升级/卸载验收。

安装回调测试可使用 `FNTERMINAL_FNPACK=/path/to/fnpack python3 -m unittest discover -s scripts/tests -v`。Bubblewrap 测试会在隔离的文件系统和用户命名空间内以 namespace-root 执行真实回调，包括解包实际 FPK，不改变宿主机 UID 或系统目录。测试环境需要 `bwrap`、`unshare` 和可用的非特权用户命名空间。

安装诊断写入 root 私有日志 `/var/log/fnterminal-install.log`（0600），并通过 `fnterminal-install` 标签发送到系统日志；同时保留平台的 `TRIM_TEMP_LOGFILE` 和标准错误输出。独立日志位于应用目录之外，避免安装失败清理应用目录时一并丢失。记录回调入口、实际 UID、限定的路径/账号环境、成功或失败步骤，不记录整个环境或凭据。只在受保护的日志路径追加，拒绝符号链接和不可信文件。

```bash
sudo tail -n 80 /var/log/fnterminal-install.log
sudo journalctl -t fnterminal-install --since "10 minutes ago" --no-pager
```

0.1.4 修复账号主目录不存在时无法启动 Shell 的问题：工作目录回退到 `/`，保留账号身份和原始 `HOME`，不自动创建主目录。连接建立前的失败会显示“连接失败”，服务日志增加 Origin、辅助进程连接与 WebSocket 升级阶段的诊断。日志位于 `/var/apps/fnterminal/var/service.log`，不记录命令、终端内容或凭据。公网访问是否还有独立的握手问题，仍需真机重试确认。

## 官方集成依据

- [统一网关与用户身份](https://developer.fnnas.com/docs/core-concepts/gateway-registration/)
- [应用运行权限](https://developer.fnnas.com/docs/core-concepts/privilege/)
- [生命周期与安装目录](https://developer.fnnas.com/docs/core-concepts/framework/)
- [安装与配置向导](https://developer.fnnas.com/docs/core-concepts/wizard/)

# internal/larkws

自 [larksuite/oapi-sdk-go v3.12.0](https://github.com/larksuite/oapi-sdk-go/tree/v3.12.0/ws)
的 `ws` 包原样拷贝(MIT License,版权与许可声明见各源文件头部),仅做以下补丁:

1. 包名 `ws` → `larkws`,避免与上游冲突;
2. `client.go`:放开被上游注释的 `WithCardHandler` 选项(字段与类型均为上游预留);
3. `client_message.go` `handleDataFrame`:上游把 `card` 类型帧直接丢弃,补丁改为分发给
   `cardHandler`,回写逻辑在 [card_dispatch.go](card_dispatch.go)。

**背景**:飞书支持通过长连接接收卡片交互回调(`card.action.trigger`,如按钮点击),
但截至 v3.12.0(含 master 分支)官方 SDK 只预留了字段、没有启用。当上游正式放开后,
应删除本目录、切回官方 `larkws` 包。

升级方式:拷贝新版 `ws` 包覆盖本目录,重新套用上述三个补丁(都有注释标记)。

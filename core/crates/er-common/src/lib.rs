//! EasyRemote 客户端核心的共享层：错误码与协议消息。
//! 本 crate 不依赖任何 IO，保证能被单元测试完整覆盖。

pub mod errors;
pub mod protocol;

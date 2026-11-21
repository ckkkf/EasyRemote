//! 会话数据面：票据、线格式、分片。
//!
//! 与 Go 服务端的协议保持一致是硬要求——
//! 任何字段顺序或长度前缀的偏差都会表现为「握手成功但数据不流动」。

pub mod backoff;
pub mod crypto;
pub mod identity;
pub mod relay_client;
pub mod ticket;
pub mod wire;

// 网络探测结果模型（STUN / NAT / 打洞）。
//
// UI 只依赖这个纯数据类，IO 实现（UDP STUN）与 Web 兜底各自产出它，
// 这样诊断页不用 import dart:io，也不会在浏览器里编译失败。
class NatProbeResult {
  const NatProbeResult({
    required this.supported,
    required this.nat,
    required this.natHint,
    required this.punch,
    required this.punchHint,
    required this.link,
    required this.linkHint,
    this.mapped = '',
    this.punchable = false,
    this.detail = '',
    this.error = '',
  });

  /// 当前平台能否做真实 UDP 探测（桌面 IO = true，浏览器 = false）。
  final bool supported;

  /// NAT 类型的人话名称，例如「锥形 NAT」「对称型 NAT」「无 NAT」。
  final String nat;
  final String natHint;

  /// 打洞结果，例如「自打洞成功」「可打洞（成功率较高）」。
  final String punch;
  final String punchHint;

  /// 当前链路结论：P2P 直连可用 / 中继转发。
  final String link;
  final String linkHint;

  /// STUN 返回的公网映射地址（ip:port）。
  final String mapped;

  /// 是否具备可打洞条件（供 UI 上色）。
  final bool punchable;

  /// 汇总成一段人话建议。
  final String detail;

  final String error;

  static const unsupported = NatProbeResult(
    supported: false,
    nat: '不支持',
    natHint: '浏览器沙箱不能发 UDP：NAT/打洞探测只在桌面客户端可用',
    punch: '不支持',
    punchHint: '需要 UDP 探测能力',
    link: '中继转发',
    linkHint: '浏览器端只能走 TCP 中继',
    detail: '浏览器沙箱不能发 UDP：NAT 类型与打洞结果需要桌面客户端实测；TCP 时延与出口地址仍然有效。',
  );

  factory NatProbeResult.blocked(String error, {String mapped = ''}) =>
      NatProbeResult(
        supported: true,
        nat: 'UDP 不可达',
        natHint: 'STUN 无响应：网络封了 UDP 或只放行 TCP',
        punch: '不可打洞',
        punchHint: 'UDP 出不去，P2P 无通道',
        link: '中继转发',
        linkHint: '只能走 TCP 中继',
        mapped: mapped,
        detail: 'STUN 探测超时：当前网络可能封了 UDP，P2P 打洞无法建立，会话会走中继转发。',
        error: error,
      );
}

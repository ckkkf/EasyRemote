// 桌面端真实网络探测：UDP STUN 取公网映射 + 跨目的地比对判 NAT + 自打洞。
//
// 为什么不用「等服务端告诉我 NAT 类型」：NAT 是**本机出口**的属性，
// 只有从本机发 UDP 才能测出来。这里用一个本地 UDP socket 向多台 STUN
// 服务器发 Binding Request，靠映射端口是否随目的地变化区分对称型 NAT。
import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'net_probe_stun.dart';
import 'net_probe_types.dart';

export 'net_probe_types.dart';

/// STUN 服务器（不同运营商的 IP，用来做「跨目的地」比对）。
const _servers = <(String, int)>[
  ('stun.l.google.com', 19302),
  ('stun.cloudflare.com', 3478),
  ('stun1.l.google.com', 19302),
];

const _perServerTimeout = Duration(seconds: 2);
const _hairpinTimeout = Duration(milliseconds: 1200);

class _Reply {
  const _Reply(this.mapped, this.rtt);
  final StunMappedAddress mapped;
  final Duration rtt;
}

class _Pending {
  _Pending(this.completer) : watch = Stopwatch()..start();
  final Completer<_Reply?> completer;
  final Stopwatch watch;
}

class _StunSession {
  _StunSession(this.socket);
  final RawDatagramSocket socket;
  final Map<String, _Pending> _pending = <String, _Pending>{};
  final Map<String, Completer<bool>> _hairpins = <String, Completer<bool>>{};
  StreamSubscription<RawSocketEvent>? _sub;
  final Random _random = Random.secure();

  void start() {
    _sub = socket.listen(_onEvent);
  }

  List<int> _txid() => List<int>.generate(12, (_) => _random.nextInt(256));

  static String _key(List<int> txid) =>
      txid.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

  Future<_Reply?> probe(
    InternetAddress address,
    int port, {
    Duration timeout = _perServerTimeout,
  }) {
    final txid = _txid();
    final key = _key(txid);
    final completer = Completer<_Reply?>();
    _pending[key] = _Pending(completer);
    socket.send(buildStunBindingRequest(txid), address, port);
    return completer.future.timeout(
      timeout,
      onTimeout: () {
        _pending.remove(key);
        return null;
      },
    );
  }

  /// 向自己的公网映射发一个 Binding Request：
  /// 能收到环回包 = NAT 支持 hairpin，打洞链路可自证。
  Future<bool> hairpin(InternetAddress address, int port) {
    final txid = _txid();
    final key = _key(txid);
    final completer = Completer<bool>();
    _hairpins[key] = completer;
    socket.send(buildStunBindingRequest(txid), address, port);
    return completer.future.timeout(
      _hairpinTimeout,
      onTimeout: () {
        _hairpins.remove(key);
        return false;
      },
    );
  }

  void _onEvent(RawSocketEvent event) {
    if (event != RawSocketEvent.read) return;
    while (true) {
      final datagram = socket.receive();
      if (datagram == null) break;
      final data = datagram.data;
      if (data.length < 20) continue;
      final txid = data.sublist(8, 20);
      final key = _key(txid);

      final pending = _pending[key];
      if (pending != null && !pending.completer.isCompleted) {
        // 只按 transaction id 认领：STUN 服务器可能从同池的另一台 anycast
        // 地址回包，按源地址过滤会把有效响应丢掉，误判成「UDP 不可达」。
        final mapped = parseStunMappedAddress(data);
        if (mapped != null) {
          pending.watch.stop();
          pending.completer.complete(_Reply(mapped, pending.watch.elapsed));
          _pending.remove(key);
          continue;
        }
      }

      // 自打洞：发往自己映射地址的请求被 NAT 环回，收到的自然是 Request。
      final hairpin = _hairpins[key];
      if (hairpin != null &&
          !hairpin.isCompleted &&
          isStunBindingRequest(data)) {
        hairpin.complete(true);
        _hairpins.remove(key);
      }
    }
  }

  Future<void> close() async {
    await _sub?.cancel();
    socket.close();
  }
}

class NetProbe {
  /// 一次完整探测：解析服务器 → 同 socket 发多台 → 判 NAT → hairpin 自检。
  static Future<NatProbeResult> probe() async {
    RawDatagramSocket? socket;
    _StunSession? session;
    try {
      final targets = await _resolveTargets();
      if (targets.isEmpty) {
        return NatProbeResult.blocked('STUN 服务器域名解析失败（检查 DNS/网络）');
      }

      socket = await RawDatagramSocket.bind(InternetAddress.anyIPv4, 0);
      socket.broadcastEnabled = false;
      session = _StunSession(socket)..start();

      final replies = await Future.wait([
        for (final t in targets) session.probe(t.$1, t.$2),
      ]);

      final ok = <_Reply>[];
      final failed = <String>[];
      for (var i = 0; i < targets.length; i++) {
        final reply = replies[i];
        if (reply == null) {
          failed.add('${targets[i].$1}:${targets[i].$2}');
        } else {
          ok.add(reply);
        }
      }
      if (ok.isEmpty) {
        return NatProbeResult.blocked('全部 STUN 服务器超时：${failed.join(', ')}');
      }

      final mapped = ok.first.mapped;
      final mappedText = mapped.toString();
      final localIps = await _localIPv4();

      // 只有拿到 ≥2 台不同目的地的映射，才能区分「端口随目的地变化」（对称型）。
      int? portA;
      int? portB;
      if (ok.length >= 2 && ok[0].mapped.ip == ok[1].mapped.ip) {
        portA = ok[0].mapped.port;
        portB = ok[1].mapped.port;
      }

      final noNat = localIps.contains(mapped.ip);
      final symmetric = portA != null && portB != null && portA != portB;
      final hairpin = await session.hairpin(
        InternetAddress(mapped.ip),
        mapped.port,
      );

      final rtts = ok.map((r) => r.rtt.inMilliseconds).toList()..sort();
      final medianRtt = rtts[rtts.length ~/ 2];

      if (noNat) {
        return NatProbeResult(
          supported: true,
          nat: '无 NAT（公网地址）',
          natHint: 'STUN 映射地址与本机网卡地址一致',
          punch: '无需打洞',
          punchHint: '本来就是公网可达地址',
          link: 'P2P 直连可用',
          linkHint: '公网地址直达，不经过中继',
          mapped: mappedText,
          punchable: true,
          detail: 'STUN 实测映射 ${mappedText} 与本机网卡地址一致：本机直接持有公网地址，P2P 直连最稳。',
        );
      }

      if (symmetric) {
        return NatProbeResult(
          supported: true,
          nat: '对称型 NAT',
          natHint: '同一 socket 到不同目的地得到不同公网端口（$portA → $portB）',
          punch: '打洞成功率低',
          punchHint: '端口随目的地变化，对方无法预测你的端口',
          link: '中继转发',
          linkHint: '对称型 NAT 下 P2P 大多失败，会话自动走中继',
          mapped: mappedText,
          punchable: false,
          detail:
              'STUN 实测：${ok.length} 台服务器响应，UDP RTT ≈ $medianRtt ms；映射端口随目的地变化（$portA → $portB），'
              '判定对称型 NAT，P2P 打洞成功率低，建议保持中继或换网络。',
        );
      }

      if (portA == null) {
        return NatProbeResult(
          supported: true,
          nat: '受限 NAT',
          natHint: '只有 ${ok.length} 台 STUN 可达，无法完成跨目的地比对',
          punch: '可尝试打洞',
          punchHint: '至少一条 UDP 路径可用',
          link: 'P2P 直连可用',
          linkHint: 'UDP 能出去，打洞可尝试',
          mapped: mappedText,
          punchable: true,
          detail:
              'STUN 实测映射 ${mappedText}，UDP RTT ≈ $medianRtt ms；仅部分 STUN 可达，'
              'NAT 类型无法细分，但 UDP 通道存在，P2P 可尝试。',
        );
      }

      return NatProbeResult(
        supported: true,
        nat: '锥形 NAT',
        natHint: '同一 socket 到不同目的地端口一致（$portA），端口可预测',
        punch: hairpin ? '自打洞成功' : '可打洞（成功率较高）',
        punchHint: hairpin
            ? '向自己的公网映射发包能收到环回，打洞链路自证通过'
            : '端口映射稳定；hairpin 被路由器拦，但不影响与对方打洞',
        link: 'P2P 直连可用',
        linkHint: '端口受限/全锥形以外的锥形 NAT，打洞成功率高',
        mapped: mappedText,
        punchable: true,
        detail:
            'STUN 实测：${ok.length} 台服务器响应，UDP RTT ≈ $medianRtt ms；映射端口在不同目的地间保持一致（$portA），'
            '判定锥形 NAT，具备打洞条件${hairpin ? '，且 hairpin 自检通过' : ''}。',
      );
    } catch (e) {
      return NatProbeResult.blocked(e.toString());
    } finally {
      await session?.close();
    }
  }

  /// 解析出至少两个不同的公网目的地。
  static Future<List<(InternetAddress, int)>> _resolveTargets() async {
    final out = <(InternetAddress, int)>[];
    final seen = <String>{};
    for (final server in _servers) {
      try {
        final list = await InternetAddress.lookup(server.$1)
            .timeout(const Duration(seconds: 2));
        for (final addr in list) {
          if (addr.type != InternetAddressType.IPv4 ||
              addr.isLoopback ||
              addr.address == '0.0.0.0') {
            continue;
          }
          if (seen.add(addr.address)) {
            out.add((addr, server.$2));
            break;
          }
        }
      } catch (_) {
        // 单台解析失败不影响其它服务器。
      }
      if (out.length >= 2) break;
    }
    return out;
  }

  static Future<Set<String>> _localIPv4() async {
    try {
      final interfaces = await NetworkInterface.list(
        type: InternetAddressType.IPv4,
        includeLinkLocal: true,
      );
      return {
        for (final i in interfaces)
          for (final a in i.addresses) a.address,
      };
    } catch (_) {
      return <String>{};
    }
  }
}

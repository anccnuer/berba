package com.berba.shell;

import java.net.InetAddress;

/** NetworkPolicy 的 JVM 单测，由 build-shell 脚本调用。 */
public final class NetworkPolicyTest {
    private NetworkPolicyTest() {
    }

    public static void main(String[] args) throws Exception {
        check(NetworkPolicy.isPrivateHost("127.0.0.1"), "loopback 应受限");
        check(NetworkPolicy.isPrivateHost("localhost"), "localhost 应受限");
        check(NetworkPolicy.isPrivateHost("10.1.2.3"), "10/8 应受限");
        check(NetworkPolicy.isPrivateHost("172.16.0.1"), "172.16/12 应受限");
        check(NetworkPolicy.isPrivateHost("192.168.1.1"), "192.168/16 应受限");
        check(NetworkPolicy.isPrivateHost("169.254.1.1"), "link-local 应受限");
        check(NetworkPolicy.isPrivateHost("100.64.0.1"), "CGNAT 应受限");
        check(NetworkPolicy.isPrivateHost("0.0.0.0"), "0/8 应受限");
        check(NetworkPolicy.isPrivateHost("::1"), "IPv6 loopback 应受限");
        check(NetworkPolicy.isPrivateHost("fc00::1"), "IPv6 ULA 应受限");
        check(NetworkPolicy.isPrivateHost("fe80::1"), "IPv6 link-local 应受限");
        check(NetworkPolicy.isPrivateHost("::ffff:192.168.0.1"), "IPv4-mapped 私网应受限");
        check(NetworkPolicy.isPrivateHost("no-such-host.invalid"), "解析失败应受限");

        check(!NetworkPolicy.isPrivate(InetAddress.getByName("8.8.8.8")), "公网 IPv4 不应受限");
        check(!NetworkPolicy.isPrivate(InetAddress.getByName("2001:4860:4860::8888")),
                "公网 IPv6 不应受限");
        System.out.println("NetworkPolicy OK");
    }

    private static void check(boolean ok, String message) {
        if (!ok) {
            throw new AssertionError(message);
        }
    }
}

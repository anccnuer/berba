package com.berba.shell;

import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * 判断主机是否落在本机/私网/链路本地等不应默认访问的范围。
 * 纯 JDK 实现，便于在 JVM 上单测。
 */
final class NetworkPolicy {
    private NetworkPolicy() {
    }

    /** 任一解析结果属于受限网段即视为受限；解析失败按受限处理。 */
    static boolean isPrivateHost(String host) {
        if (host == null || host.trim().isEmpty()) {
            return true;
        }
        try {
            return isPrivateHost(InetAddress.getAllByName(host));
        } catch (UnknownHostException e) {
            return true;
        }
    }

    static boolean isPrivateHost(InetAddress[] addresses) {
        if (addresses == null || addresses.length == 0) {
            return true;
        }
        for (InetAddress address : addresses) {
            if (isPrivate(address)) {
                return true;
            }
        }
        return false;
    }

    static boolean isPrivate(InetAddress address) {
        if (address.isAnyLocalAddress()
                || address.isLoopbackAddress()
                || address.isLinkLocalAddress()
                || address.isSiteLocalAddress()
                || address.isMulticastAddress()) {
            return true;
        }
        byte[] b = address.getAddress();
        if (b.length == 4) {
            return isPrivateV4(b, 0);
        }
        if (b.length != 16) {
            return true;
        }
        // fc00::/7 unique local
        if ((b[0] & 0xFE) == 0xFC) {
            return true;
        }
        // ::ffff:0:0/96 IPv4-mapped
        boolean mapped = true;
        for (int i = 0; i < 10; i++) {
            if (b[i] != 0) {
                mapped = false;
                break;
            }
        }
        if (mapped && (b[10] & 0xFF) == 0xFF && (b[11] & 0xFF) == 0xFF) {
            return isPrivateV4(b, 12);
        }
        return false;
    }

    private static boolean isPrivateV4(byte[] b, int off) {
        int a = b[off] & 0xFF;
        int c = b[off + 1] & 0xFF;
        if (a == 0) {
            return true; // 0.0.0.0/8
        }
        if (a == 100 && c >= 64 && c <= 127) {
            return true; // 100.64.0.0/10 CGNAT
        }
        if (a == 192 && c == 0 && (b[off + 2] & 0xFF) == 0) {
            return true; // 192.0.0.0/24 IETF
        }
        if (a == 198 && (c == 18 || c == 19)) {
            return true; // 198.18.0.0/15 benchmarking
        }
        return false;
    }
}

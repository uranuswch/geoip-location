# geoip-location

China IP 离线数据库，以 MaxMind MMDB 格式发布。

每日自动从多个开源数据源拉取、合并、去重后生成。

## 数据源

| 数据源 | 格式 | 说明 |
|--------|------|------|
| [sapics/ip-location-db](https://github.com/sapics/ip-location-db) | CSV | GeoLite2 + WHOIS ASN 数据，过滤 CN 记录 |
| [gaoyifan/china-operator-ip](https://github.com/gaoyifan/china-operator-ip) | CIDR | 中国运营商 BGP 地址段，含 IPv6 |
| [17mon/china_ip_list](https://github.com/17mon/china_ip_list) | CIDR | IPIP.net 发布的中国 IP 列表 |
| [metowolf/qqwry.dat](https://github.com/metowolf/qqwry.dat) | 二进制 | 纯真 IP 数据库，提取中国地址段 |

## 下载

从 [release 分支](../../tree/release) 获取最新版本：

- `China-only.mmdb` — MMDB 数据库文件
- `China-only.mmdb.sha256` — SHA256 校验文件

## 使用

兼容所有支持 MaxMind GeoIP2 格式的软件（Clash、Surge、libmaxminddb 等）。

### Go

```go
db, _ := maxminddb.Open("China-only.mmdb")
defer db.Close()

addr, _ := netip.ParseAddr("114.114.114.114")
result := db.Lookup(addr)
if result.Found() {
    fmt.Println("This is a China IP")
}
```

### Python

```python
import maxminddb

with maxminddb.open_database("China-only.mmdb") as db:
    result = db.get("114.114.114.114")
    if result:
        print(result["country"]["iso_code"])  # CN
```

### Clash

```yaml
geodata: true
geox-url:
  geoip: "https://raw.githubusercontent.com/uranuswch/geoip-location/release/China-only.mmdb"
```

## 本地构建

```bash
go build -o geoip-gen ./cmd/geoip-gen/
./geoip-gen
```

生成的文件：
- `China-only.mmdb`
- `China-only.mmdb.sha256`

## 自动更新

GitHub Actions workflow（`.github/workflows/update.yml`）每日 UTC 00:17 自动执行：

1. 拉取所有数据源
2. 解析、合并、去重
3. 生成 MMDB + SHA256
4. 推送到 `release` 分支

支持手动触发（workflow_dispatch）。

## 输出格式

- 数据库类型：`GeoIP2-Country`
- IP 版本：IPv4 + IPv6（双栈）
- 记录内容：`{"country": {"iso_code": "CN", "geoname_id": 1814991}}`

## License

数据来源于各开源项目，请参考各数据源的原始许可证。

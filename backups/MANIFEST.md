# Backup manifest

Full backup of the **Rayswitch RSFM101** (Rockchip RK3126, Android 6.0.1),
taken 2026-11-22 before any changes. Root shell via shipped SuperSU `su`.

Device: serial `JMIBF91BUZ`, USB `2207:0006`, Frameo v1.29.14 (beta v1.32.18).

Fingerprint: `rockchip/rk312x/rk312x:6.0.1/MXC89K/user.zcx.20260312.081202:user/release-keys`

## Partition images (`backups/*.img`)

Source: `/dev/block/platform/1021c000.rksdmmc/by-name/*` (eMMC `mmcblk0`, 32 GB).

| File | Partition | Size (bytes) | Matches /proc/partitions |
| --- | --- | --- | --- |
| uboot.img | p1 uboot | 4,194,304 | ✓ 4096 KiB |
| misc.img | p2 misc | 4,194,304 | ✓ 4096 KiB |
| resource.img | p3 resource | 16,777,216 | ✓ 16384 KiB |
| kernel.img | p4 kernel | 12,582,912 | ✓ 12288 KiB |
| boot.img | p5 boot | 23,068,672 | ✓ 22528 KiB |
| recovery.img | p6 recovery | 33,554,432 | ✓ 32768 KiB |
| backup.img | p7 backup | 67,108,864 | ✓ 65536 KiB |
| metadata.img | p9 metadata | 16,777,216 | ✓ 16384 KiB |
| kpanic.img | p10 kpanic | 4,194,304 | ✓ 4096 KiB |
| radical_update.img | p12 radical_update | 67,108,864 | ✓ 65536 KiB |
| system.img | p11 system | 1,207,959,552 | ✓ 1179648 KiB |

Not backed up: `userdata` (p13, ~28 GB, mostly empty) and `cache` (p8).
`userdata` holds photos/user data; back it up separately before a factory reset.

## Pulled artifacts

- `build.prop` — `/system/build.prop`
- `apks/com.adups.fota.apk` — Adups FOTA (`/system/app/FotaUpdate/FotaUpdate.apk`)
- `apks/net.frameo.frame.apk` — Frameo stock (`/system/priv-app/frameo/frameo.apk`)
- `apks/net.frameo.frame-beta.apk` — Frameo beta v1.32.18 (`/data/app/net.frameo.frame-1/base.apk`)
- `apks/com.cghs.stresstest.apk` — factory stress test (`/system/priv-app/StressTest/StressTest.apk`)
- `apks/InputDevices.apk` — `/system/priv-app/InputDevices/InputDevices.apk`

## Integrity

`SHA256SUMS` contains SHA-256 hashes of every file above.

## Restore

The APKs are still in `/system` (the debloat uses `pm uninstall --user 0`,
which does not delete the on-disk APK). To restore a partition:

```sh
adb push system.img /data/local/tmp/system.img
adb shell su 0 dd if=/data/local/tmp/system.img of=/dev/block/platform/1021c000.rksdmmc/by-name/system bs=4096
```

A full eMMC restore image was **not** taken (would be ~32 GB). The critical
boot chain + `/system` are covered above; that's what matters for recovery from
a software-level mistake.

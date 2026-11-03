package com.synthoscollective.node;

import android.app.AlarmManager;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ServiceInfo;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
import android.os.SystemClock;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Log;

import androidx.annotation.Nullable;
import androidx.core.app.NotificationCompat;

import org.bouncycastle.crypto.generators.Ed25519KeyPairGenerator;
import org.bouncycastle.crypto.params.Ed25519KeyGenerationParameters;
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters;
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters;
import org.bouncycastle.crypto.signers.Ed25519Signer;
import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

public class NodeForegroundService extends Service {
    private static final String TAG = "SynthosNodeService";
    public static final String ACTION_STATUS_UPDATE = "com.synthoscollective.node.STATUS_UPDATE";
    public static final String ACTION_HEARTBEAT_PULSE = "com.synthoscollective.node.HEARTBEAT_PULSE";
    public static final String CHANNEL_ID = "synthos_validator_channel";
    public static final int NOTIFICATION_ID = 2026;
    private static final int ALARM_REQ_CODE = 4040;

    private static final String RELAY_URL = "https://82-29-54-7.sslip.io";
    private static final int HEARTBEAT_INTERVAL_SEC = 15;

    public static boolean isRunning = false;
    public static long startTime = 0;
    public static long heartbeatCount = 0;
    public static long currentHeight = 0;
    public static String nodeId = "";
    public static volatile boolean identityDecryptionFailed = false;

    private PowerManager.WakeLock wakeLock;
    private WifiManager.WifiLock wifiLock;
    private ScheduledExecutorService executor;
    private Ed25519PrivateKeyParameters privateKey;
    private String publicKeyHex = "";
    private volatile boolean isRegistered = false;
    private final java.util.concurrent.atomic.AtomicBoolean isHeartbeatRunning = new java.util.concurrent.atomic.AtomicBoolean(false);

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
        initOrLoadKeys();

        // 1. CPU WakeLock - Non reference counted so it never drops accidentally
        try {
            PowerManager powerManager = (PowerManager) getSystemService(Context.POWER_SERVICE);
            if (powerManager != null) {
                wakeLock = powerManager.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "SynthosNode::ValidatorCpuLock");
                wakeLock.setReferenceCounted(false);
                wakeLock.acquire();
            }
        } catch (Exception e) {
            Log.e(TAG, "Failed to acquire CPU WakeLock: " + e.getMessage());
        }

        // 2. High-Performance Wi-Fi Lock - Prevents Android from putting Wi-Fi radio to sleep
        try {
            WifiManager wifiManager = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
            if (wifiManager != null) {
                int mode = WifiManager.WIFI_MODE_FULL_HIGH_PERF;
                wifiLock = wifiManager.createWifiLock(mode, "SynthosNode::ValidatorWifiLock");
                wifiLock.setReferenceCounted(false);
                wifiLock.acquire();
            }
        } catch (Exception e) {
            Log.e(TAG, "Failed to acquire Wi-Fi Lock: " + e.getMessage());
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_HEARTBEAT_PULSE.equals(intent.getAction())) {
            // Hardware Alarm triggered to pierce Android deep Doze mode
            if (isRunning) {
                triggerPulseAsync();
                scheduleNextAlarm();
            }
            return START_STICKY;
        }

        // Initialize and start background loop
        isRunning = true;
        if (startTime == 0) {
            startTime = System.currentTimeMillis();
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, buildNotification("SYNTHOS Node Initializing..."), ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(NOTIFICATION_ID, buildNotification("SYNTHOS Node Initializing..."));
        }

        if (executor == null || executor.isShutdown()) {
            executor = Executors.newSingleThreadScheduledExecutor();
            executor.scheduleWithFixedDelay(this::runHeartbeatCycle, 0, HEARTBEAT_INTERVAL_SEC, TimeUnit.SECONDS);
        }

        // Hardware alarm failsafe (runs every 15s to keep heartbeat alive even if executor sleeps)
        scheduleNextAlarm();
        broadcastUpdate();
        return START_STICKY;
    }

    private void scheduleNextAlarm() {
        try {
            AlarmManager alarmManager = (AlarmManager) getSystemService(Context.ALARM_SERVICE);
            if (alarmManager == null) return;

            Intent intent = new Intent(this, NodeForegroundService.class);
            intent.setAction(ACTION_HEARTBEAT_PULSE);
            intent.setPackage(getPackageName());
            int flags = PendingIntent.FLAG_UPDATE_CURRENT;
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                flags |= PendingIntent.FLAG_IMMUTABLE;
            }

            PendingIntent pi;
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                pi = PendingIntent.getForegroundService(this, ALARM_REQ_CODE, intent, flags);
            } else {
                pi = PendingIntent.getService(this, ALARM_REQ_CODE, intent, flags);
            }

            long triggerAt = SystemClock.elapsedRealtime() + (HEARTBEAT_INTERVAL_SEC * 1000L);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                if (alarmManager.canScheduleExactAlarms()) {
                    alarmManager.setExactAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pi);
                } else {
                    alarmManager.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pi);
                }
            } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                alarmManager.setExactAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pi);
            } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.KITKAT) {
                alarmManager.setExact(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pi);
            } else {
                alarmManager.set(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pi);
            }
        } catch (Exception e) {
            Log.w(TAG, "Alarm schedule: " + e.getMessage());
        }
    }

    private void triggerPulseAsync() {
        new Thread(() -> {
            PowerManager.WakeLock pulseLock = null;
            try {
                PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
                if (pm != null) {
                    pulseLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "SynthosNode::PulseLock");
                    pulseLock.acquire(25000L);
                }
                runHeartbeatCycle();
            } catch (Exception e) {
                Log.w(TAG, "Pulse execution: " + e.getMessage());
            } finally {
                if (pulseLock != null && pulseLock.isHeld()) {
                    try { pulseLock.release(); } catch (Exception ignored) {}
                }
            }
        }).start();
    }

    private synchronized void runHeartbeatCycle() {
        if (!isHeartbeatRunning.compareAndSet(false, true)) {
            return;
        }
        try {
            // Guard: ensure signing keys are unlocked and available before doing any network cycles
            if (privateKey == null) {
                initOrLoadKeys();
                if (privateKey == null) {
                    String errorMsg = identityDecryptionFailed
                            ? "Keystore Locked / Decryption Error (Keys Preserved)"
                            : "Candidate Identity Not Initialized";
                    Log.w(TAG, "Heartbeat cycle skipped: " + errorMsg);
                    NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
                    if (nm != null) {
                        nm.notify(NOTIFICATION_ID, buildNotification("SYNTHOS Paused: " + errorMsg));
                    }
                    broadcastUpdate();
                    return;
                }
            }

            // Refresh locks if somehow lost
            if (wakeLock != null && !wakeLock.isHeld()) wakeLock.acquire();
            if (wifiLock != null && !wifiLock.isHeld()) wifiLock.acquire();

            // 1. Fetch chain tip
            JSONObject status = httpGet(RELAY_URL + "/status");
            long height = 0;
            String tip = "";
            String stateRoot = "";
            if (status != null) {
                height = status.optLong("height", 0);
                tip = status.optString("tip", "");
                stateRoot = status.optString("state_root", "");
                currentHeight = height;
            }

            // 2. Register node if not yet registered or periodically every 50 heartbeats
            if (!isRegistered || heartbeatCount % 50 == 0) {
                JSONObject regPayload = new JSONObject();
                regPayload.put("publicId", nodeId);
                regPayload.put("public_key", publicKeyHex);
                regPayload.put("mode", "public-validator");
                regPayload.put("role", "validator_candidate");
                regPayload.put("network", "mainnet");
                regPayload.put("endpoint", "");
                regPayload.put("background", true);
                regPayload.put("hardware_commitment", "android-" + nodeId);
                regPayload.put("machine_commitment", "android-" + nodeId);
                JSONArray caps = new JSONArray();
                caps.put("immune_node");
                caps.put("economist");
                caps.put("governor");
                caps.put("communicator");
                caps.put("simulator");
                caps.put("enforcer");
                caps.put("citizen");
                regPayload.put("capabilities", caps);
                if (httpPost(RELAY_URL + "/api/nodes/register", regPayload.toString())) {
                    isRegistered = true;
                }
            }

            // 3. Build & sign heartbeat
            heartbeatCount++;
            long nowMs = System.currentTimeMillis();
            String nonce = String.format(Locale.US, "%019d-%08d", nowMs, heartbeatCount);

            SimpleDateFormat sdf = new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US);
            sdf.setTimeZone(TimeZone.getTimeZone("UTC"));
            String timestamp = sdf.format(new Date(nowMs));

            String message = String.format(
                    Locale.US,
                    "SYNTHOS_HEARTBEAT_V1\nnode_id=%s\nheight=%d\ntip=%s\nstate_root=%s\ntimestamp=%s\nnonce=%s",
                    nodeId, height, tip, stateRoot, timestamp, nonce
            );

            Ed25519Signer signer = new Ed25519Signer();
            signer.init(true, privateKey);
            byte[] msgBytes = message.getBytes(StandardCharsets.UTF_8);
            signer.update(msgBytes, 0, msgBytes.length);
            byte[] signatureBytes = signer.generateSignature();
            String signatureHex = bytesToHex(signatureBytes);

            JSONObject hbPayload = new JSONObject();
            hbPayload.put("node_id", nodeId);
            hbPayload.put("height", height);
            hbPayload.put("tip", tip);
            hbPayload.put("state_root", stateRoot);
            hbPayload.put("timestamp", timestamp);
            hbPayload.put("nonce", nonce);
            hbPayload.put("signature", signatureHex);
            JSONArray caps = new JSONArray();
            caps.put("immune_node");
            caps.put("citizen");
            hbPayload.put("capabilities", caps);

            httpPost(RELAY_URL + "/api/nodes/heartbeat", hbPayload.toString());

            // 4. Update notification & broadcast
            String notifText = String.format(Locale.US, "Reporting Uptime Height #%d • Heartbeats: %d", currentHeight, heartbeatCount);
            NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
            if (nm != null) {
                nm.notify(NOTIFICATION_ID, buildNotification(notifText));
            }

            broadcastUpdate();
        } catch (Exception e) {
            Log.e(TAG, "Heartbeat cycle error: " + e.getMessage(), e);
        } finally {
            isHeartbeatRunning.set(false);
        }
    }

    private void broadcastUpdate() {
        Intent intent = new Intent(ACTION_STATUS_UPDATE);
        intent.setPackage(getPackageName());
        intent.putExtra("isRunning", isRunning);
        intent.putExtra("heartbeats", heartbeatCount);
        intent.putExtra("height", currentHeight);
        intent.putExtra("nodeId", nodeId);
        intent.putExtra("startTime", startTime);
        intent.putExtra("identityDecryptionFailed", identityDecryptionFailed);
        sendBroadcast(intent);
    }

    private Notification buildNotification(String text) {
        Intent notifIntent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                this, 0, notifIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );

        return new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle("SYNTHOS Candidate Node Active (0.5% CPU)")
                .setContentText(text)
                .setSmallIcon(R.drawable.ic_launcher_foreground)
                .setContentIntent(pendingIntent)
                .setOngoing(true)
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .build();
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    "SYNTHOS Candidate Node Channel",
                    NotificationManager.IMPORTANCE_LOW
            );
            channel.setDescription("Shows active Proof-of-Uptime candidate status");
            NotificationManager manager = getSystemService(NotificationManager.class);
            if (manager != null) {
                manager.createNotificationChannel(channel);
            }
        }
    }

    private static final String KEYSTORE_PROVIDER = "AndroidKeyStore";
    private static final String MASTER_KEY_ALIAS = "synthos_candidate_identity_master_key";
    private static final String AES_GCM_TRANSFORMATION = "AES/GCM/NoPadding";
    private static final int GCM_TAG_LENGTH = 128;

    private SecretKey getOrCreateMasterKey() throws Exception {
        KeyStore keyStore = KeyStore.getInstance(KEYSTORE_PROVIDER);
        keyStore.load(null);
        if (!keyStore.containsAlias(MASTER_KEY_ALIAS)) {
            KeyGenerator keyGenerator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE_PROVIDER);
            KeyGenParameterSpec spec = new KeyGenParameterSpec.Builder(
                    MASTER_KEY_ALIAS,
                    KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .setKeySize(256)
                    .build();
            keyGenerator.init(spec);
            keyGenerator.generateKey();
        }
        return (SecretKey) keyStore.getKey(MASTER_KEY_ALIAS, null);
    }

    private byte[] encryptIdentityData(byte[] plaintext) {
        try {
            SecretKey key = getOrCreateMasterKey();
            Cipher cipher = Cipher.getInstance(AES_GCM_TRANSFORMATION);
            cipher.init(Cipher.ENCRYPT_MODE, key);
            byte[] iv = cipher.getIV();
            byte[] ciphertext = cipher.doFinal(plaintext);
            JSONObject container = new JSONObject();
            container.put("iv", bytesToHex(iv));
            container.put("ciphertext", bytesToHex(ciphertext));
            return container.toString().getBytes(StandardCharsets.UTF_8);
        } catch (Exception e) {
            Log.w(TAG, "Hardware Keystore encryption unavailable, falling back: " + e.getMessage());
            return plaintext;
        }
    }

    private byte[] decryptIdentityData(byte[] rawData) throws Exception {
        String str = new String(rawData, StandardCharsets.UTF_8);
        if (str.startsWith("{") && str.contains("ciphertext") && str.contains("iv")) {
            JSONObject container = new JSONObject(str);
            byte[] iv = hexToBytes(container.getString("iv"));
            byte[] ciphertext = hexToBytes(container.getString("ciphertext"));
            SecretKey key = getOrCreateMasterKey();
            Cipher cipher = Cipher.getInstance(AES_GCM_TRANSFORMATION);
            GCMParameterSpec spec = new GCMParameterSpec(GCM_TAG_LENGTH, iv);
            cipher.init(Cipher.DECRYPT_MODE, key, spec);
            return cipher.doFinal(ciphertext);
        }
        if (str.startsWith("{") && str.contains("private_key")) {
            return rawData;
        }
        throw new IllegalStateException("Unrecognized identity file format");
    }

    private synchronized void initOrLoadKeys() {
        // Protected, non-backup storage directory (excluded from all OS cloud and local backups)
        File keyFile = new File(getNoBackupFilesDir(), "candidate_identity.dat");
        boolean keyFileExisted = keyFile.exists() && keyFile.length() > 0;
        String savedPrivHex = null;
        String savedPubHex = null;
        String savedNodeId = null;
        boolean decryptionAttemptFailed = false;

        if (keyFileExisted) {
            try (FileInputStream fis = new FileInputStream(keyFile)) {
                byte[] fileBytes = new byte[(int) keyFile.length()];
                int read = fis.read(fileBytes);
                if (read > 0) {
                    byte[] decrypted = decryptIdentityData(fileBytes);
                    if (decrypted == null) {
                        throw new IllegalStateException("Decryption returned null");
                    }
                    JSONObject obj = new JSONObject(new String(decrypted, StandardCharsets.UTF_8));
                    savedPrivHex = obj.optString("private_key", null);
                    savedPubHex = obj.optString("public_key", null);
                    savedNodeId = obj.optString("node_id", null);
                    if (savedPrivHex == null || savedPubHex == null || savedNodeId == null) {
                        throw new IllegalStateException("Incomplete identity payload");
                    }
                }
            } catch (Exception e) {
                decryptionAttemptFailed = true;
                Log.e(TAG, "CRITICAL: Error reading or decrypting protected identity file: " + e.getMessage(), e);
            }
        }

        // Migration fallback: check legacy SharedPreferences ONLY if keyFile never existed
        boolean isMigration = false;
        SharedPreferences legacyPrefs = null;
        if (!keyFileExisted) {
            legacyPrefs = getSharedPreferences("synthos_node_keys", MODE_PRIVATE);
            savedPrivHex = legacyPrefs.getString("private_key", null);
            savedPubHex = legacyPrefs.getString("public_key", null);
            savedNodeId = legacyPrefs.getString("node_id", null);
            if (savedPrivHex != null) {
                isMigration = true;
            }
        }

        if (savedPrivHex != null && savedPubHex != null && savedNodeId != null) {
            privateKey = new Ed25519PrivateKeyParameters(hexToBytes(savedPrivHex), 0);
            publicKeyHex = savedPubHex;
            nodeId = savedNodeId;
            identityDecryptionFailed = false;

            // Ensure encrypted and persisted into protected no-backup file
            if (!keyFileExisted || isMigration) {
                boolean written = persistProtectedIdentity(keyFile, savedPrivHex, savedPubHex, savedNodeId);
                // Delete from legacy SharedPreferences ONLY AFTER atomic file write succeeds!
                if (written && isMigration && legacyPrefs != null) {
                    legacyPrefs.edit().remove("private_key").remove("public_key").remove("node_id").apply();
                    Log.i(TAG, "Successfully migrated candidate identity to hardware-encrypted protected storage");
                }
            }
        } else if (keyFileExisted || decryptionAttemptFailed) {
            // SAFETY GUARANTEE: An existing identity was present on this device, but could not be decrypted.
            // Under NO circumstance should we silently generate a new identity and overwrite the existing file!
            identityDecryptionFailed = true;
            Log.e(TAG, "IDENTITY PROTECTION LOCK: Existing candidate identity exists but Keystore decryption failed. Preserving file and refusing to overwrite.");
        } else {
            // Truly a first-time clean install: neither keyFile nor legacy SharedPreferences ever existed.
            try {
                Ed25519KeyPairGenerator keyPairGen = new Ed25519KeyPairGenerator();
                keyPairGen.init(new Ed25519KeyGenerationParameters(new SecureRandom()));
                org.bouncycastle.crypto.AsymmetricCipherKeyPair keyPair = keyPairGen.generateKeyPair();

                privateKey = (Ed25519PrivateKeyParameters) keyPair.getPrivate();
                Ed25519PublicKeyParameters pubKey = (Ed25519PublicKeyParameters) keyPair.getPublic();

                byte[] privBytes = privateKey.getEncoded();
                byte[] pubBytes = pubKey.getEncoded();

                publicKeyHex = bytesToHex(pubBytes);
                nodeId = "syn-android-" + publicKeyHex.substring(0, Math.min(12, publicKeyHex.length()));
                identityDecryptionFailed = false;

                persistProtectedIdentity(keyFile, bytesToHex(privBytes), publicKeyHex, nodeId);
            } catch (Exception e) {
                Log.e(TAG, "Failed to generate initial candidate identity: " + e.getMessage(), e);
                identityDecryptionFailed = true;
            }
        }
    }

    private boolean persistProtectedIdentity(File targetFile, String privHex, String pubHex, String nId) {
        File tempFile = new File(targetFile.getParentFile(), targetFile.getName() + ".tmp");
        try {
            JSONObject obj = new JSONObject();
            obj.put("private_key", privHex);
            obj.put("public_key", pubHex);
            obj.put("node_id", nId);
            byte[] plaintext = obj.toString().getBytes(StandardCharsets.UTF_8);
            byte[] payload = encryptIdentityData(plaintext);

            try (FileOutputStream fos = new FileOutputStream(tempFile)) {
                fos.write(payload);
                fos.flush();
                fos.getFD().sync();
            }

            if (!tempFile.renameTo(targetFile)) {
                targetFile.delete();
                if (!tempFile.renameTo(targetFile)) {
                    Log.e(TAG, "Atomic rename of temp identity file failed");
                    return false;
                }
            }
            return true;
        } catch (Exception e) {
            Log.e(TAG, "Failed to persist identity in protected storage: " + e.getMessage(), e);
            if (tempFile.exists()) tempFile.delete();
            return false;
        }
    }

    private JSONObject httpGet(String urlStr) {
        HttpURLConnection conn = null;
        try {
            URL url = new URL(urlStr);
            conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("GET");
            conn.setConnectTimeout(8000);
            conn.setReadTimeout(8000);
            conn.setRequestProperty("User-Agent", "SynthosAndroidNode/1.0");
            conn.setRequestProperty("Connection", "close");
            int respCode = conn.getResponseCode();
            if (respCode == 200) {
                BufferedReader reader = new BufferedReader(new InputStreamReader(conn.getInputStream()));
                StringBuilder sb = new StringBuilder();
                String line;
                while ((line = reader.readLine()) != null) sb.append(line);
                reader.close();
                return new JSONObject(sb.toString());
            } else {
                Log.w(TAG, "httpGet failed with code " + respCode + " for " + urlStr);
            }
        } catch (Exception e) {
            Log.e(TAG, "httpGet error for " + urlStr + ": " + e.getMessage());
        } finally {
            if (conn != null) conn.disconnect();
        }
        return null;
    }

    private boolean httpPost(String urlStr, String jsonBody) {
        HttpURLConnection conn = null;
        try {
            URL url = new URL(urlStr);
            conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("POST");
            conn.setConnectTimeout(8000);
            conn.setReadTimeout(8000);
            conn.setRequestProperty("User-Agent", "SynthosAndroidNode/1.0");
            conn.setRequestProperty("Content-Type", "application/json");
            conn.setRequestProperty("Connection", "close");
            conn.setDoOutput(true);
            try (OutputStream os = conn.getOutputStream()) {
                os.write(jsonBody.getBytes(StandardCharsets.UTF_8));
            }
            int code = conn.getResponseCode();
            if (code >= 200 && code < 300) {
                return true;
            } else {
                Log.w(TAG, "httpPost failed with code " + code + " for " + urlStr);
            }
        } catch (Exception e) {
            Log.e(TAG, "httpPost error for " + urlStr + ": " + e.getMessage());
        } finally {
            if (conn != null) conn.disconnect();
        }
        return false;
    }

    private static String bytesToHex(byte[] bytes) {
        StringBuilder sb = new StringBuilder();
        for (byte b : bytes) {
            sb.append(String.format("%02x", b));
        }
        return sb.toString();
    }

    private static byte[] hexToBytes(String hex) {
        int len = hex.length();
        byte[] data = new byte[len / 2];
        for (int i = 0; i < len; i += 2) {
            data[i / 2] = (byte) ((Character.digit(hex.charAt(i), 16) << 4)
                    + Character.digit(hex.charAt(i + 1), 16));
        }
        return data;
    }

    @Override
    public void onDestroy() {
        isRunning = false;
        startTime = 0;
        if (executor != null) {
            executor.shutdownNow();
        }
        try {
            AlarmManager alarmManager = (AlarmManager) getSystemService(Context.ALARM_SERVICE);
            if (alarmManager != null) {
                Intent intent = new Intent(this, NodeForegroundService.class);
                intent.setAction(ACTION_HEARTBEAT_PULSE);
                int flags = PendingIntent.FLAG_UPDATE_CURRENT;
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    flags |= PendingIntent.FLAG_IMMUTABLE;
                }
                PendingIntent pi = PendingIntent.getService(this, ALARM_REQ_CODE, intent, flags);
                alarmManager.cancel(pi);
            }
        } catch (Exception ignored) {}

        try {
            if (wakeLock != null && wakeLock.isHeld()) {
                wakeLock.release();
            }
        } catch (Exception ignored) {}
        try {
            if (wifiLock != null && wifiLock.isHeld()) {
                wifiLock.release();
            }
        } catch (Exception ignored) {}
        broadcastUpdate();
        super.onDestroy();
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}

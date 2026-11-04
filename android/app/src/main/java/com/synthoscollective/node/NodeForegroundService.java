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

    public static final String PREFS_NAME = "synthos_node_state";
    public static final String KEY_IS_RUNNING = "is_running";
    public static final String KEY_SESSION_START = "session_start_time";
    public static final String KEY_ACCUMULATED_UPTIME = "accumulated_uptime_sec";
    public static final String KEY_HEARTBEATS = "heartbeats";
    public static final String KEY_HEIGHT = "height";
    public static final String KEY_NODE_ID = "node_id";

    public static final String RELAY_URL = "https://82-29-54-7.sslip.io";
    private static final int HEARTBEAT_INTERVAL_SEC = 15;

    public static volatile NodeForegroundService instance = null;
    public static boolean isRunning = false;
    public static long sessionStartTime = 0;
    public static long accumulatedUptimeSec = 0;
    public static long heartbeatCount = 0;
    public static long currentHeight = 0;
    public static String nodeId = "";
    public static volatile boolean identityDecryptionFailed = false;

    private PowerManager.WakeLock wakeLock;
    private WifiManager.WifiLock wifiLock;
    private ScheduledExecutorService executor;
    private static Ed25519PrivateKeyParameters privateKey;
    private static String publicKeyHex = "";
    private volatile boolean isRegistered = false;
    private final java.util.concurrent.atomic.AtomicBoolean isHeartbeatRunning = new java.util.concurrent.atomic.AtomicBoolean(false);

    public static void loadPersistedState(Context context) {
        try {
            SharedPreferences prefs = context.getSharedPreferences(PREFS_NAME, MODE_PRIVATE);
            isRunning = prefs.getBoolean(KEY_IS_RUNNING, isRunning);
            if (sessionStartTime == 0) {
                sessionStartTime = prefs.getLong(KEY_SESSION_START, 0);
            }
            if (accumulatedUptimeSec == 0) {
                accumulatedUptimeSec = prefs.getLong(KEY_ACCUMULATED_UPTIME, 0);
            }
            if (heartbeatCount == 0) {
                heartbeatCount = prefs.getLong(KEY_HEARTBEATS, 0);
            }
            if (currentHeight == 0) {
                currentHeight = prefs.getLong(KEY_HEIGHT, 0);
            }
            if (nodeId == null || nodeId.isEmpty()) {
                nodeId = prefs.getString(KEY_NODE_ID, "");
            }
        } catch (Exception e) {
            Log.w(TAG, "Failed loading state: " + e.getMessage());
        }
    }

    public void saveState() {
        try {
            SharedPreferences prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE);
            prefs.edit()
                    .putBoolean(KEY_IS_RUNNING, isRunning)
                    .putLong(KEY_SESSION_START, sessionStartTime)
                    .putLong(KEY_ACCUMULATED_UPTIME, accumulatedUptimeSec)
                    .putLong(KEY_HEARTBEATS, heartbeatCount)
                    .putLong(KEY_HEIGHT, currentHeight)
                    .putString(KEY_NODE_ID, nodeId)
                    .apply();
        } catch (Exception e) {
            Log.w(TAG, "Failed persisting state: " + e.getMessage());
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        instance = this;
        createNotificationChannel();
        loadPersistedState(this);
        initOrLoadKeys(this);
        acquireLocks();
    }

    private synchronized void acquireLocks() {
        try {
            if (wakeLock == null) {
                PowerManager powerManager = (PowerManager) getSystemService(Context.POWER_SERVICE);
                if (powerManager != null) {
                    wakeLock = powerManager.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "SynthosNode::ValidatorCpuLock");
                    wakeLock.setReferenceCounted(false);
                }
            }
            if (wakeLock != null && !wakeLock.isHeld()) {
                wakeLock.acquire();
                Log.d(TAG, "Acquired CPU WakeLock");
            }
        } catch (Exception e) {
            Log.e(TAG, "Failed to acquire CPU WakeLock: " + e.getMessage());
        }

        try {
            if (wifiLock == null) {
                WifiManager wifiManager = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
                if (wifiManager != null) {
                    wifiLock = wifiManager.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "SynthosNode::ValidatorWifiLock");
                    wifiLock.setReferenceCounted(false);
                }
            }
            if (wifiLock != null && !wifiLock.isHeld()) {
                wifiLock.acquire();
                Log.d(TAG, "Acquired Wi-Fi Lock");
            }
        } catch (Exception e) {
            Log.e(TAG, "Failed to acquire Wi-Fi Lock: " + e.getMessage());
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, buildNotification("SYNTHOS Node Active"), ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(NOTIFICATION_ID, buildNotification("SYNTHOS Node Active"));
        }

        instance = this;
        isRunning = true;
        loadPersistedState(this);
        if (sessionStartTime == 0) {
            sessionStartTime = System.currentTimeMillis();
        }
        saveState();
        acquireLocks();

        if (executor == null || executor.isShutdown()) {
            executor = Executors.newSingleThreadScheduledExecutor();
            executor.scheduleWithFixedDelay(this::runHeartbeatCycle, 0, HEARTBEAT_INTERVAL_SEC, TimeUnit.SECONDS);
        }

        scheduleNextAlarm(this);
        broadcastUpdate();
        return START_STICKY;
    }

    public static void scheduleNextAlarm(Context context) {
        try {
            AlarmManager alarmManager = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
            if (alarmManager == null) return;

            Intent intent = new Intent(context, AlarmReceiver.class);
            intent.setAction(ACTION_HEARTBEAT_PULSE);
            intent.setPackage(context.getPackageName());
            int flags = PendingIntent.FLAG_UPDATE_CURRENT;
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                flags |= PendingIntent.FLAG_IMMUTABLE;
            }

            PendingIntent pi = PendingIntent.getBroadcast(context, ALARM_REQ_CODE, intent, flags);
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

    public static void triggerPulseAsync(Context context) {
        new Thread(() -> {
            PowerManager.WakeLock pulseLock = null;
            try {
                PowerManager pm = (PowerManager) context.getSystemService(Context.POWER_SERVICE);
                if (pm != null) {
                    pulseLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "SynthosNode::PulseLock");
                    pulseLock.acquire(30000L);
                }
                if (instance != null) {
                    instance.runHeartbeatCycle();
                } else {
                    Intent serviceIntent = new Intent(context, NodeForegroundService.class);
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                        context.startForegroundService(serviceIntent);
                    } else {
                        context.startService(serviceIntent);
                    }
                }
            } catch (Exception e) {
                Log.w(TAG, "Pulse execution: " + e.getMessage());
            } finally {
                if (pulseLock != null && pulseLock.isHeld()) {
                    try { pulseLock.release(); } catch (Exception ignored) {}
                }
            }
        }).start();
    }

    public synchronized void runHeartbeatCycle() {
        if (!isHeartbeatRunning.compareAndSet(false, true)) {
            return;
        }
        try {
            if (privateKey == null) {
                initOrLoadKeys(this);
                if (privateKey == null) {
                    String errorMsg = identityDecryptionFailed
                            ? "Keystore Locked / Decryption Error"
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

            acquireLocks();

            // 1. Fetch chain tip
            JSONObject status = httpGet(RELAY_URL + "/status");
            long height = 0;
            String tip = "";
            String stateRoot = "";
            if (status != null) {
                height = status.optLong("height", 0);
                tip = status.optString("tip", "");
                stateRoot = status.optString("state_root", "");
                if (height > 0) {
                    currentHeight = height;
                }
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

            // 4. Save state & update notification & broadcast
            saveState();

            String notifText = String.format(Locale.US, "Height #%,d • Heartbeats: %,d", currentHeight, heartbeatCount);
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
        intent.putExtra("sessionStartTime", sessionStartTime);
        intent.putExtra("accumulatedUptimeSec", accumulatedUptimeSec);
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
                .setPriority(NotificationCompat.PRIORITY_HIGH)
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

    private static SecretKey getOrCreateMasterKey() throws Exception {
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

    private static byte[] encryptIdentityData(byte[] plaintext) {
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
            Log.w(TAG, "Hardware Keystore encryption fallback: " + e.getMessage());
            return plaintext;
        }
    }

    private static byte[] decryptIdentityData(byte[] rawData) throws Exception {
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

    public static synchronized void initOrLoadKeys(Context context) {
        if (privateKey != null && nodeId != null && !nodeId.isEmpty()) {
            return;
        }
        File keyFile = new File(context.getNoBackupFilesDir(), "candidate_identity.dat");
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
                Log.e(TAG, "CRITICAL: Error decrypting identity file: " + e.getMessage(), e);
            }
        }

        boolean isMigration = false;
        SharedPreferences legacyPrefs = null;
        if (!keyFileExisted) {
            legacyPrefs = context.getSharedPreferences("synthos_node_keys", MODE_PRIVATE);
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

            if (!keyFileExisted || isMigration) {
                boolean written = persistProtectedIdentity(keyFile, savedPrivHex, savedPubHex, savedNodeId);
                if (written && isMigration && legacyPrefs != null) {
                    legacyPrefs.edit().remove("private_key").remove("public_key").remove("node_id").apply();
                }
            }
        } else if (keyFileExisted || decryptionAttemptFailed) {
            identityDecryptionFailed = true;
            Log.e(TAG, "IDENTITY PROTECTION LOCK: Existing candidate identity exists but Keystore decryption failed.");
        } else {
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
                Log.e(TAG, "Failed generating initial identity: " + e.getMessage(), e);
                identityDecryptionFailed = true;
            }
        }

        // Always ensure nodeId is persisted into state
        if (nodeId != null && !nodeId.isEmpty()) {
            try {
                context.getSharedPreferences(PREFS_NAME, MODE_PRIVATE).edit().putString(KEY_NODE_ID, nodeId).apply();
            } catch (Exception ignored) {}
        }
    }

    private static boolean persistProtectedIdentity(File targetFile, String privHex, String pubHex, String nId) {
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
                    return false;
                }
            }
            return true;
        } catch (Exception e) {
            Log.e(TAG, "Failed persisting identity: " + e.getMessage(), e);
            if (tempFile.exists()) tempFile.delete();
            return false;
        }
    }

    public static JSONObject httpGet(String urlStr) {
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
            }
        } catch (Exception e) {
            Log.w(TAG, "httpGet error for " + urlStr + ": " + e.getMessage());
        } finally {
            if (conn != null) conn.disconnect();
        }
        return null;
    }

    public static boolean httpPost(String urlStr, String jsonBody) {
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
            return code >= 200 && code < 300;
        } catch (Exception e) {
            Log.w(TAG, "httpPost error for " + urlStr + ": " + e.getMessage());
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
        if (isRunning && sessionStartTime > 0) {
            long sessionSec = (System.currentTimeMillis() - sessionStartTime) / 1000;
            accumulatedUptimeSec += Math.max(0, sessionSec);
            sessionStartTime = 0;
        }
        instance = null;
        isRunning = false;
        saveState();
        if (executor != null) {
            executor.shutdownNow();
        }
        try {
            AlarmManager alarmManager = (AlarmManager) getSystemService(Context.ALARM_SERVICE);
            if (alarmManager != null) {
                Intent intent = new Intent(this, AlarmReceiver.class);
                intent.setAction(ACTION_HEARTBEAT_PULSE);
                int flags = PendingIntent.FLAG_UPDATE_CURRENT;
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    flags |= PendingIntent.FLAG_IMMUTABLE;
                }
                PendingIntent pi = PendingIntent.getBroadcast(this, ALARM_REQ_CODE, intent, flags);
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

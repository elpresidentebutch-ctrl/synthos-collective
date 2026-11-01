package com.synthoscollective.node;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
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
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

public class NodeForegroundService extends Service {
    private static final String TAG = "SynthosNodeService";
    public static final String ACTION_STATUS_UPDATE = "com.synthoscollective.node.STATUS_UPDATE";
    public static final String CHANNEL_ID = "synthos_validator_channel";
    public static final int NOTIFICATION_ID = 2026;

    private static final String RELAY_URL = "https://82-29-54-7.sslip.io";
    private static final int HEARTBEAT_INTERVAL_SEC = 15;

    public static boolean isRunning = false;
    public static long startTime = 0;
    public static long heartbeatCount = 0;
    public static long currentHeight = 0;
    public static String nodeId = "";

    private PowerManager.WakeLock wakeLock;
    private ScheduledExecutorService executor;
    private Ed25519PrivateKeyParameters privateKey;
    private String publicKeyHex = "";

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
        initOrLoadKeys();

        PowerManager powerManager = (PowerManager) getSystemService(Context.POWER_SERVICE);
        if (powerManager != null) {
            wakeLock = powerManager.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "SynthosNode::ValidatorLock");
            wakeLock.acquire();
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (!isRunning) {
            isRunning = true;
            startTime = System.currentTimeMillis();
            startForeground(NOTIFICATION_ID, buildNotification("SYNTHOS Node Initializing..."));

            executor = Executors.newSingleThreadScheduledExecutor();
            executor.scheduleWithFixedDelay(this::runHeartbeatCycle, 0, HEARTBEAT_INTERVAL_SEC, TimeUnit.SECONDS);
        }
        return START_STICKY;
    }

    private void runHeartbeatCycle() {
        try {
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

            // 2. Register node if needed
            if (heartbeatCount == 0 || heartbeatCount % 100 == 0) {
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
                httpPost(RELAY_URL + "/api/nodes/register", regPayload.toString());
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
            String notifText = String.format(Locale.US, "Validating Height #%d • Heartbeats: %d", currentHeight, heartbeatCount);
            NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
            if (nm != null) {
                nm.notify(NOTIFICATION_ID, buildNotification(notifText));
            }

            broadcastUpdate();
        } catch (Exception e) {
            Log.e(TAG, "Heartbeat cycle error: " + e.getMessage(), e);
        }
    }

    private void broadcastUpdate() {
        Intent intent = new Intent(ACTION_STATUS_UPDATE);
        intent.putExtra("isRunning", isRunning);
        intent.putExtra("heartbeats", heartbeatCount);
        intent.putExtra("height", currentHeight);
        intent.putExtra("nodeId", nodeId);
        intent.putExtra("startTime", startTime);
        sendBroadcast(intent);
    }

    private Notification buildNotification(String text) {
        Intent notifIntent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                this, 0, notifIntent,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ? PendingIntent.FLAG_IMMUTABLE : 0)
        );

        return new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle("SYNTHOS Node Active (0.5% CPU)")
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
                    "SYNTHOS Validator Channel",
                    NotificationManager.IMPORTANCE_LOW
            );
            channel.setDescription("Shows active background validation status");
            NotificationManager manager = getSystemService(NotificationManager.class);
            if (manager != null) {
                manager.createNotificationChannel(channel);
            }
        }
    }

    private void initOrLoadKeys() {
        SharedPreferences prefs = getSharedPreferences("synthos_node_keys", MODE_PRIVATE);
        String savedPrivHex = prefs.getString("private_key", null);
        String savedPubHex = prefs.getString("public_key", null);
        String savedNodeId = prefs.getString("node_id", null);

        if (savedPrivHex != null && savedPubHex != null && savedNodeId != null) {
            privateKey = new Ed25519PrivateKeyParameters(hexToBytes(savedPrivHex), 0);
            publicKeyHex = savedPubHex;
            nodeId = savedNodeId;
        } else {
            Ed25519KeyPairGenerator keyPairGen = new Ed25519KeyPairGenerator();
            keyPairGen.init(new Ed25519KeyGenerationParameters(new SecureRandom()));
            org.bouncycastle.crypto.AsymmetricCipherKeyPair keyPair = keyPairGen.generateKeyPair();

            privateKey = (Ed25519PrivateKeyParameters) keyPair.getPrivate();
            Ed25519PublicKeyParameters pubKey = (Ed25519PublicKeyParameters) keyPair.getPublic();

            byte[] privBytes = privateKey.getEncoded();
            byte[] pubBytes = pubKey.getEncoded();

            publicKeyHex = bytesToHex(pubBytes);
            nodeId = "syn-android-" + publicKeyHex.substring(0, Math.min(12, publicKeyHex.length()));

            prefs.edit()
                    .putString("private_key", bytesToHex(privBytes))
                    .putString("public_key", publicKeyHex)
                    .putString("node_id", nodeId)
                    .apply();
        }
    }

    private JSONObject httpGet(String urlStr) {
        try {
            URL url = new URL(urlStr);
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("GET");
            conn.setConnectTimeout(8000);
            conn.setReadTimeout(8000);
            if (conn.getResponseCode() == 200) {
                BufferedReader reader = new BufferedReader(new InputStreamReader(conn.getInputStream()));
                StringBuilder sb = new StringBuilder();
                String line;
                while ((line = reader.readLine()) != null) sb.append(line);
                reader.close();
                return new JSONObject(sb.toString());
            }
        } catch (Exception ignored) {}
        return null;
    }

    private boolean httpPost(String urlStr, String jsonBody) {
        try {
            URL url = new URL(urlStr);
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("POST");
            conn.setConnectTimeout(8000);
            conn.setReadTimeout(8000);
            conn.setRequestProperty("Content-Type", "application/json");
            conn.setDoOutput(true);
            try (OutputStream os = conn.getOutputStream()) {
                os.write(jsonBody.getBytes(StandardCharsets.UTF_8));
            }
            int code = conn.getResponseCode();
            return code >= 200 && code < 300;
        } catch (Exception ignored) {}
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
        if (executor != null) {
            executor.shutdownNow();
        }
        if (wakeLock != null && wakeLock.isHeld()) {
            wakeLock.release();
        }
        broadcastUpdate();
        super.onDestroy();
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}

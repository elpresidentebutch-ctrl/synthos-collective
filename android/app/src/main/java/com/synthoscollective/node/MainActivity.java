package com.synthoscollective.node;

import android.Manifest;
import android.annotation.SuppressLint;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.provider.Settings;
import android.view.View;
import android.widget.Button;
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;
import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;
import androidx.core.content.pm.ShortcutInfoCompat;
import androidx.core.content.pm.ShortcutManagerCompat;
import androidx.core.graphics.drawable.IconCompat;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.Locale;

public class MainActivity extends AppCompatActivity {
    private static final int PERMISSION_REQ_NOTIF = 101;

    private View statusIndicator;
    private TextView statusText;
    private TextView nodeIdText;
    private TextView heightText;
    private TextView heartbeatsText;
    private TextView uptimeText;
    private Button toggleButton;
    private Button pinHomeButton;
    private Button batteryOptButton;

    private final Handler uptimeHandler = new Handler(Looper.getMainLooper());

    private final BroadcastReceiver updateReceiver = new BroadcastReceiver() {
        @Override
        public void onReceive(Context context, Intent intent) {
            if (intent != null) {
                if (intent.hasExtra("isRunning")) {
                    NodeForegroundService.isRunning = intent.getBooleanExtra("isRunning", NodeForegroundService.isRunning);
                }
                if (intent.hasExtra("heartbeats")) {
                    NodeForegroundService.heartbeatCount = intent.getLongExtra("heartbeats", NodeForegroundService.heartbeatCount);
                }
                if (intent.hasExtra("height")) {
                    long h = intent.getLongExtra("height", 0);
                    if (h > 0) NodeForegroundService.currentHeight = h;
                }
                if (intent.hasExtra("nodeId")) {
                    String nId = intent.getStringExtra("nodeId");
                    if (nId != null && !nId.isEmpty()) NodeForegroundService.nodeId = nId;
                }
                if (intent.hasExtra("sessionStartTime")) {
                    long st = intent.getLongExtra("sessionStartTime", 0);
                    if (st > 0) NodeForegroundService.sessionStartTime = st;
                }
                if (intent.hasExtra("accumulatedUptimeSec")) {
                    long acc = intent.getLongExtra("accumulatedUptimeSec", 0);
                    if (acc > 0) NodeForegroundService.accumulatedUptimeSec = acc;
                }
                if (intent.hasExtra("identityDecryptionFailed")) {
                    NodeForegroundService.identityDecryptionFailed = intent.getBooleanExtra("identityDecryptionFailed", false);
                }
            }
            updateUI();
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);

        statusIndicator = findViewById(R.id.statusIndicator);
        statusText = findViewById(R.id.statusText);
        nodeIdText = findViewById(R.id.nodeIdText);
        heightText = findViewById(R.id.heightText);
        heartbeatsText = findViewById(R.id.heartbeatsText);
        uptimeText = findViewById(R.id.uptimeText);
        toggleButton = findViewById(R.id.toggleButton);
        pinHomeButton = findViewById(R.id.pinHomeButton);
        batteryOptButton = findViewById(R.id.batteryOptButton);

        toggleButton.setOnClickListener(v -> toggleNodeService());
        pinHomeButton.setOnClickListener(v -> pinToHomeScreen());
        batteryOptButton.setOnClickListener(v -> requestBatteryOptimizationExemption());

        // 1. Initialize persistent state & node identity so UI is never blank
        NodeForegroundService.initOrLoadKeys(this);
        NodeForegroundService.loadPersistedState(this);

        checkNotificationPermission();
        updateUI();
        startUptimeTicker();
        fetchLiveNetworkStatsAsync();

        // 2. Auto-resume candidate service if previously enabled
        SharedPreferences statePrefs = getSharedPreferences(NodeForegroundService.PREFS_NAME, MODE_PRIVATE);
        boolean shouldRun = statePrefs.getBoolean(NodeForegroundService.KEY_IS_RUNNING, false)
                || getSharedPreferences("synthos_node_keys", MODE_PRIVATE).getBoolean(BootReceiver.PREF_NODE_ENABLED, false);

        if (shouldRun) {
            NodeForegroundService.isRunning = true;
            Intent serviceIntent = new Intent(this, NodeForegroundService.class);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                startForegroundService(serviceIntent);
            } else {
                startService(serviceIntent);
            }
        }
    }

    private void fetchLiveNetworkStatsAsync() {
        new Thread(() -> {
            try {
                // 1. Fetch current network block height from relay
                JSONObject status = NodeForegroundService.httpGet(NodeForegroundService.RELAY_URL + "/status");
                if (status != null) {
                    long h = status.optLong("height", 0);
                    if (h > 0) {
                        NodeForegroundService.currentHeight = Math.max(NodeForegroundService.currentHeight, h);
                        getSharedPreferences(NodeForegroundService.PREFS_NAME, MODE_PRIVATE)
                                .edit().putLong(NodeForegroundService.KEY_HEIGHT, NodeForegroundService.currentHeight).apply();
                    }
                }

                // 2. Query node's own registered stats on relay
                String currentId = NodeForegroundService.nodeId;
                if (currentId != null && !currentId.isEmpty()) {
                    JSONObject nodesObj = NodeForegroundService.httpGet(NodeForegroundService.RELAY_URL + "/api/nodes");
                    if (nodesObj != null && nodesObj.has("peers")) {
                        JSONArray peers = nodesObj.optJSONArray("peers");
                        if (peers != null) {
                            for (int i = 0; i < peers.length(); i++) {
                                JSONObject p = peers.optJSONObject(i);
                                if (p != null && currentId.equals(p.optString("name", ""))) {
                                    long serverHb = p.optLong("valid_heartbeats", 0);
                                    long serverUptimeMs = p.optLong("verified_uptime_ms", 0);
                                    long serverHeight = p.optLong("height", 0);

                                    if (serverHb > NodeForegroundService.heartbeatCount) {
                                        NodeForegroundService.heartbeatCount = serverHb;
                                    }
                                    if (serverHeight > NodeForegroundService.currentHeight) {
                                        NodeForegroundService.currentHeight = serverHeight;
                                    }
                                    if ((serverUptimeMs / 1000) > NodeForegroundService.accumulatedUptimeSec) {
                                        NodeForegroundService.accumulatedUptimeSec = serverUptimeMs / 1000;
                                    }

                                    getSharedPreferences(NodeForegroundService.PREFS_NAME, MODE_PRIVATE).edit()
                                            .putLong(NodeForegroundService.KEY_HEARTBEATS, NodeForegroundService.heartbeatCount)
                                            .putLong(NodeForegroundService.KEY_HEIGHT, NodeForegroundService.currentHeight)
                                            .putLong(NodeForegroundService.KEY_ACCUMULATED_UPTIME, NodeForegroundService.accumulatedUptimeSec)
                                            .apply();
                                    break;
                                }
                            }
                        }
                    }
                }

                runOnUiThread(this::updateUI);
            } catch (Exception ignored) {}
        }).start();
    }

    private void toggleNodeService() {
        Intent serviceIntent = new Intent(this, NodeForegroundService.class);
        SharedPreferences legacyPrefs = getSharedPreferences("synthos_node_keys", MODE_PRIVATE);
        SharedPreferences statePrefs = getSharedPreferences(NodeForegroundService.PREFS_NAME, MODE_PRIVATE);

        if (NodeForegroundService.isRunning) {
            // Accumulate uptime from current session
            if (NodeForegroundService.sessionStartTime > 0) {
                long currentSessionSec = (System.currentTimeMillis() - NodeForegroundService.sessionStartTime) / 1000;
                NodeForegroundService.accumulatedUptimeSec += Math.max(0, currentSessionSec);
                NodeForegroundService.sessionStartTime = 0;
            }

            stopService(serviceIntent);
            NodeForegroundService.isRunning = false;

            legacyPrefs.edit().putBoolean(BootReceiver.PREF_NODE_ENABLED, false).apply();
            statePrefs.edit()
                    .putBoolean(NodeForegroundService.KEY_IS_RUNNING, false)
                    .putLong(NodeForegroundService.KEY_SESSION_START, 0)
                    .putLong(NodeForegroundService.KEY_ACCUMULATED_UPTIME, NodeForegroundService.accumulatedUptimeSec)
                    .apply();

            updateUI();
            Toast.makeText(this, "SYNTHOS Candidate Node Stopped", Toast.LENGTH_SHORT).show();
        } else {
            requestBatteryOptimizationExemption();

            NodeForegroundService.isRunning = true;
            NodeForegroundService.sessionStartTime = System.currentTimeMillis();

            legacyPrefs.edit().putBoolean(BootReceiver.PREF_NODE_ENABLED, true).apply();
            statePrefs.edit()
                    .putBoolean(NodeForegroundService.KEY_IS_RUNNING, true)
                    .putLong(NodeForegroundService.KEY_SESSION_START, NodeForegroundService.sessionStartTime)
                    .apply();

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                startForegroundService(serviceIntent);
            } else {
                startService(serviceIntent);
            }

            // Immediately trigger an async heartbeat pulse so user sees live update without waiting 15s
            NodeForegroundService.triggerPulseAsync(this);

            updateUI();
            Toast.makeText(this, "SYNTHOS Candidate Node Started (0.5% CPU)", Toast.LENGTH_SHORT).show();
        }
    }

    private void updateUI() {
        boolean running = NodeForegroundService.isRunning;
        if (NodeForegroundService.identityDecryptionFailed) {
            statusIndicator.setBackgroundColor(ContextCompat.getColor(this, R.color.danger_red));
            statusText.setText("KEYSTORE LOCKED / ERROR");
            statusText.setTextColor(ContextCompat.getColor(this, R.color.danger_red));
            toggleButton.setText("RETRY NODE");
            toggleButton.setBackgroundTintList(ContextCompat.getColorStateList(this, R.color.accent_teal));
        } else if (running) {
            statusIndicator.setBackgroundColor(ContextCompat.getColor(this, R.color.accent_teal));
            statusText.setText(R.string.status_validating);
            statusText.setTextColor(ContextCompat.getColor(this, R.color.accent_teal));
            toggleButton.setText("STOP CANDIDATE NODE");
            toggleButton.setBackgroundTintList(ContextCompat.getColorStateList(this, R.color.danger_red));
        } else {
            statusIndicator.setBackgroundColor(ContextCompat.getColor(this, R.color.danger_red));
            statusText.setText(R.string.status_stopped);
            statusText.setTextColor(ContextCompat.getColor(this, R.color.danger_red));
            toggleButton.setText("START CANDIDATE NODE");
            toggleButton.setBackgroundTintList(ContextCompat.getColorStateList(this, R.color.accent_teal));
        }

        if (NodeForegroundService.nodeId != null && !NodeForegroundService.nodeId.isEmpty()) {
            nodeIdText.setText(NodeForegroundService.nodeId);
        } else {
            nodeIdText.setText("syn-android-...");
        }

        if (NodeForegroundService.currentHeight > 0) {
            heightText.setText(String.format(Locale.US, "#%,d", NodeForegroundService.currentHeight));
        } else {
            heightText.setText("Connecting...");
        }

        heartbeatsText.setText(String.format(Locale.US, "%,d", NodeForegroundService.heartbeatCount));

        // Format verified cumulative uptime
        long currentSessionSec = (running && NodeForegroundService.sessionStartTime > 0)
                ? (System.currentTimeMillis() - NodeForegroundService.sessionStartTime) / 1000
                : 0;
        long totalUptimeSec = NodeForegroundService.accumulatedUptimeSec + Math.max(0, currentSessionSec);

        if (totalUptimeSec >= 86400) {
            long days = totalUptimeSec / 86400;
            long rem = totalUptimeSec % 86400;
            long hours = rem / 3600;
            long minutes = (rem % 3600) / 60;
            long seconds = rem % 60;
            uptimeText.setText(String.format(Locale.US, "%dd %02d:%02d:%02d", days, hours, minutes, seconds));
        } else {
            long hours = totalUptimeSec / 3600;
            long minutes = (totalUptimeSec % 3600) / 60;
            long seconds = totalUptimeSec % 60;
            uptimeText.setText(String.format(Locale.US, "%02d:%02d:%02d", hours, minutes, seconds));
        }
    }

    private void startUptimeTicker() {
        uptimeHandler.postDelayed(new Runnable() {
            @Override
            public void run() {
                updateUI();
                uptimeHandler.postDelayed(this, 1000);
            }
        }, 1000);
    }

    private void checkNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                    != PackageManager.PERMISSION_GRANTED) {
                ActivityCompat.requestPermissions(
                        this,
                        new String[]{Manifest.permission.POST_NOTIFICATIONS},
                        PERMISSION_REQ_NOTIF
                );
            }
        }
    }

    @SuppressLint("BatteryLife")
    private void requestBatteryOptimizationExemption() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
            if (pm != null && !pm.isIgnoringBatteryOptimizations(getPackageName())) {
                Intent intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
                intent.setData(Uri.parse("package:" + getPackageName()));
                try {
                    startActivity(intent);
                } catch (Exception e) {
                    Toast.makeText(this, "Open Battery Settings to allow background activity", Toast.LENGTH_LONG).show();
                }
            } else {
                Toast.makeText(this, "Battery is set to Unrestricted for 24/7 background validation!", Toast.LENGTH_SHORT).show();
            }
        }
    }

    private void pinToHomeScreen() {
        if (ShortcutManagerCompat.isRequestPinShortcutSupported(this)) {
            Intent launchIntent = new Intent(this, MainActivity.class);
            launchIntent.setAction(Intent.ACTION_MAIN);

            ShortcutInfoCompat pinShortcutInfo = new ShortcutInfoCompat.Builder(this, "synthos_home_shortcut")
                    .setIcon(IconCompat.createWithResource(this, R.mipmap.ic_launcher))
                    .setShortLabel("SYNTHOS Node")
                    .setLongLabel("SYNTHOS Validator Node")
                    .setIntent(launchIntent)
                    .build();

            ShortcutManagerCompat.requestPinShortcut(this, pinShortcutInfo, null);
            Toast.makeText(this, "Adding SYNTHOS Node icon to your Home Screen...", Toast.LENGTH_SHORT).show();
        } else {
            Toast.makeText(this, "Home screen icon is already in your app drawer", Toast.LENGTH_SHORT).show();
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        NodeForegroundService.loadPersistedState(this);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(updateReceiver, new IntentFilter(NodeForegroundService.ACTION_STATUS_UPDATE), Context.RECEIVER_NOT_EXPORTED);
        } else {
            registerReceiver(updateReceiver, new IntentFilter(NodeForegroundService.ACTION_STATUS_UPDATE));
        }
        updateUI();
        fetchLiveNetworkStatsAsync();
    }

    @Override
    protected void onPause() {
        super.onPause();
        try {
            unregisterReceiver(updateReceiver);
        } catch (Exception ignored) {}
    }

    @Override
    protected void onDestroy() {
        uptimeHandler.removeCallbacksAndMessages(null);
        super.onDestroy();
    }
}

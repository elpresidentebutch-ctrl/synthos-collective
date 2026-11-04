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

import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;
import androidx.core.content.pm.ShortcutInfoCompat;
import androidx.core.content.pm.ShortcutManagerCompat;
import androidx.core.graphics.drawable.IconCompat;

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
                    NodeForegroundService.currentHeight = intent.getLongExtra("height", NodeForegroundService.currentHeight);
                }
                if (intent.hasExtra("nodeId")) {
                    String nId = intent.getStringExtra("nodeId");
                    if (nId != null && !nId.isEmpty()) NodeForegroundService.nodeId = nId;
                }
                if (intent.hasExtra("startTime")) {
                    long st = intent.getLongExtra("startTime", 0);
                    if (st > 0) NodeForegroundService.startTime = st;
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
        NodeForegroundService.loadPersistedState(this);
        NodeForegroundService.initOrLoadKeys(this);

        checkNotificationPermission();
        updateUI();
        startUptimeTicker();

        // 2. Auto-resume candidate service if previously running
        if (NodeForegroundService.isRunning) {
            Intent serviceIntent = new Intent(this, NodeForegroundService.class);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                startForegroundService(serviceIntent);
            } else {
                startService(serviceIntent);
            }
        }
    }

    private void toggleNodeService() {
        Intent serviceIntent = new Intent(this, NodeForegroundService.class);
        SharedPreferences legacyPrefs = getSharedPreferences("synthos_node_keys", MODE_PRIVATE);
        SharedPreferences statePrefs = getSharedPreferences(NodeForegroundService.PREFS_NAME, MODE_PRIVATE);

        if (NodeForegroundService.isRunning) {
            stopService(serviceIntent);
            NodeForegroundService.isRunning = false;
            legacyPrefs.edit().putBoolean(BootReceiver.PREF_NODE_ENABLED, false).apply();
            statePrefs.edit().putBoolean(NodeForegroundService.KEY_IS_RUNNING, false).apply();
            updateUI();
            Toast.makeText(this, "SYNTHOS Candidate Node Stopped", Toast.LENGTH_SHORT).show();
        } else {
            requestBatteryOptimizationExemption();
            legacyPrefs.edit().putBoolean(BootReceiver.PREF_NODE_ENABLED, true).apply();
            statePrefs.edit().putBoolean(NodeForegroundService.KEY_IS_RUNNING, true).apply();
            NodeForegroundService.isRunning = true;
            if (NodeForegroundService.startTime == 0) {
                NodeForegroundService.startTime = System.currentTimeMillis();
                statePrefs.edit().putLong(NodeForegroundService.KEY_START_TIME, NodeForegroundService.startTime).apply();
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                startForegroundService(serviceIntent);
            } else {
                startService(serviceIntent);
            }
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
            nodeIdText.setText("Initializing...");
        }

        if (NodeForegroundService.currentHeight > 0) {
            heightText.setText(String.format(Locale.US, "#%,d", NodeForegroundService.currentHeight));
        } else {
            heightText.setText("---");
        }

        heartbeatsText.setText(String.format(Locale.US, "%,d", NodeForegroundService.heartbeatCount));
    }

    private void startUptimeTicker() {
        uptimeHandler.postDelayed(new Runnable() {
            @Override
            public void run() {
                updateUI();
                if (NodeForegroundService.isRunning && NodeForegroundService.startTime > 0) {
                    long diff = System.currentTimeMillis() - NodeForegroundService.startTime;
                    long seconds = (diff / 1000) % 60;
                    long minutes = (diff / (1000 * 60)) % 60;
                    long hours = (diff / (1000 * 60 * 60));
                    uptimeText.setText(String.format(Locale.US, "%02d:%02d:%02d", hours, minutes, seconds));
                } else {
                    uptimeText.setText("00:00:00");
                }
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

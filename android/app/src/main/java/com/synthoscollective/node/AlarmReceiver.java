package com.synthoscollective.node;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.Build;
import android.util.Log;

public class AlarmReceiver extends BroadcastReceiver {
    private static final String TAG = "SynthosAlarmReceiver";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null) return;
        Log.d(TAG, "Heartbeat alarm received in background: " + intent.getAction());

        SharedPreferences statePrefs = context.getSharedPreferences(NodeForegroundService.PREFS_NAME, Context.MODE_PRIVATE);
        boolean shouldBeRunning = statePrefs.getBoolean(NodeForegroundService.KEY_IS_RUNNING, false)
                || context.getSharedPreferences("synthos_node_keys", Context.MODE_PRIVATE).getBoolean(BootReceiver.PREF_NODE_ENABLED, false);

        if (NodeForegroundService.isRunning) {
            NodeForegroundService.triggerPulseAsync(context.getApplicationContext());
            NodeForegroundService.scheduleNextAlarm(context.getApplicationContext());
        } else if (shouldBeRunning) {
            Log.i(TAG, "Service was not running but marked enabled, restarting foreground service");
            Intent serviceIntent = new Intent(context, NodeForegroundService.class);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(serviceIntent);
            } else {
                context.startService(serviceIntent);
            }
        }
    }
}

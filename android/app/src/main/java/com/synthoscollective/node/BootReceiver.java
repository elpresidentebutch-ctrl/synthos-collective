package com.synthoscollective.node;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.Build;

public class BootReceiver extends BroadcastReceiver {
    public static final String PREF_NODE_ENABLED = "node_enabled";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction()) ||
                "android.intent.action.QUICKBOOT_POWERON".equals(intent.getAction())) {

            // Only start the background service if the user explicitly opted in / turned the node on
            SharedPreferences prefs = context.getSharedPreferences("synthos_node_keys", Context.MODE_PRIVATE);
            boolean nodeEnabled = prefs.getBoolean(PREF_NODE_ENABLED, false);

            if (nodeEnabled) {
                Intent serviceIntent = new Intent(context, NodeForegroundService.class);
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(serviceIntent);
                } else {
                    context.startService(serviceIntent);
                }
            }
        }
    }
}

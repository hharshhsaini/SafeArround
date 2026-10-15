module.exports = () => ({
  expo: {
    name: 'SafeAround',
    slug: 'safearound',
    version: '1.0.0',
    orientation: 'portrait',
    icon: './assets/icon.png',
    userInterfaceStyle: 'light',
    splash: {
      image: './assets/icon.png',
      resizeMode: 'contain',
      backgroundColor: '#F2F6FF',
    },
    ios: {
      supportsTablet: false,
      bundleIdentifier: 'com.safearound.app',
    },
    android: {
      package: 'com.safearound.app',
      googleServicesFile: './google-services.json',
      permissions: [
        'ACCESS_FINE_LOCATION',
        'ACCESS_COARSE_LOCATION',
        'ACCESS_BACKGROUND_LOCATION',
        'FOREGROUND_SERVICE',
        'FOREGROUND_SERVICE_LOCATION',
        'ACTIVITY_RECOGNITION',
        'CAMERA',
        'RECORD_AUDIO',
        'VIBRATE',
        'POST_NOTIFICATIONS',
        'READ_CONTACTS',
      ],
      // The app only reads contacts (to pick emergency contacts).
      blockedPermissions: ['android.permission.WRITE_CONTACTS'],
    },
    web: {
      favicon: './assets/favicon.png',
    },
    plugins: [
      'expo-location',
      'expo-notifications',
      'expo-camera',
      'expo-secure-store',
      [
        'expo-contacts',
        {
          contactsPermission:
            'SafeAround uses your contacts so you can choose who to alert in an emergency.',
        },
      ],
      'expo-font',
    ],
    extra: {
      eas: {
        projectId: '7ce2c65d-8aab-4188-8404-183e8c7f2317',
      },
    },
  },
});

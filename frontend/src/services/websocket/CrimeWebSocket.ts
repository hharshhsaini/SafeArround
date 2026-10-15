import { useAuthStore } from '@/store/authStore';

// React Native's WebSocket accepts request headers as a third argument, but the
// DOM typings in Expo's tsconfig only describe the browser's two-argument form.
type NativeWebSocketConstructor = new (
  url: string,
  protocols: string | string[] | null,
  options: { headers: Record<string, string> },
) => WebSocket;

class CrimeWebSocketService {
  private socket: WebSocket | null = null;
  private listeners: Map<string, Function[]> = new Map();
  private reconnectTimer: any = null;
  private serverUrl: string = '';
  private shouldReconnect: boolean = false;

  connect(serverUrl: string) {
    if (this.socket && (this.socket.readyState === WebSocket.OPEN || this.socket.readyState === WebSocket.CONNECTING)) {
      return;
    }
    if (!serverUrl || !/^wss?:\/\//i.test(serverUrl)) {
      console.warn('Crime websocket startup skipped because the URL is invalid.');
      return;
    }
    this.serverUrl = serverUrl;
    this.shouldReconnect = true;

    // The server only accepts signed-in sockets. Read the token on every attempt
    // so a reconnect uses the latest token the API client has refreshed.
    const accessToken = useAuthStore.getState().accessToken;
    if (!accessToken) {
      this.scheduleReconnect();
      return;
    }

    try {
      this.socket = new (WebSocket as unknown as NativeWebSocketConstructor)(serverUrl, null, {
        headers: { Authorization: `Bearer ${accessToken}` },
      });
    } catch (error) {
      console.warn('Crime websocket could not be created right now; retry will happen later.', error);
      this.socket = null;
      this.scheduleReconnect();
      return;
    }

    this.socket.onopen = () => {
      console.log('✅ Connected to crime updates via native WebSocket');
      if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    };

    this.socket.onclose = () => {
      console.log('❌ Disconnected from crime updates');
      this.scheduleReconnect();
    };

    this.socket.onerror = (error) => {
      console.warn('WebSocket connection unavailable; realtime updates will retry in the background.', error);
    };

    // Listen for crime updates and emergency alerts from server
    this.socket.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        const { event: eventName, data: eventData } = payload;
        
        switch (eventName) {
          case 'crime_added':
          case 'new_crime':
            this.emit('crime_added', eventData || payload);
            break;
          case 'crime_batch':
            this.emit('crime_batch', eventData || payload);
            break;
          case 'emergency_alert':
            this.emit('emergency_alert', eventData);
            break;
          case 'responder_accepted':
            this.emit('responder_accepted', eventData);
            break;
          case 'radius_expanded':
            this.emit('radius_expanded', eventData);
            break;
          case 'connected':
            if (eventData) {
              console.log('WS Connection data:', eventData);
            }
            break;
          case 'room_closed':
            this.emit('room_closed', eventData);
            break;
          default:
            console.log('Unhandled WS event:', eventName);
        }
      } catch (error) {
        console.error('WebSocket message parse error:', error);
      }
    };
  }

  on(event: string, callback: Function) {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, []);
    }
    this.listeners.get(event)!.push(callback);
  }

  off(event: string, callback: Function) {
    const callbacks = this.listeners.get(event);
    if (callbacks) {
      const index = callbacks.indexOf(callback);
      if (index > -1) {
        callbacks.splice(index, 1);
      }
    }
  }

  private scheduleReconnect() {
    if (!this.shouldReconnect) {
      return;
    }
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.reconnectTimer = setTimeout(() => this.connect(this.serverUrl), 3000);
  }

  private emit(event: string, data: any) {
    const callbacks = this.listeners.get(event) || [];
    callbacks.forEach(cb => cb(data));
  }

  disconnect() {
    this.shouldReconnect = false;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.socket) {
      this.socket.close();
      this.socket = null;
    }
  }
}

export default new CrimeWebSocketService();

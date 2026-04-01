import { createApp, ref, onMounted, onUnmounted } from '/admin/static/vendor/vue.esm-browser.prod.js';

function updateTopbar(status) {
  const el = document.getElementById('topbar-status');
  if (!el) return;
  const dot = el.querySelector('[aria-hidden="true"]');

  // Remove all state classes
  el.classList.remove('text-success', 'text-warning', 'text-error');
  if (dot) dot.classList.remove('bg-success', 'bg-warning', 'bg-error');

  if (status === 'ok') {
    el.classList.add('text-success');
    if (dot) dot.classList.add('bg-success');
    el.setAttribute('aria-label', 'System status: OK');
  } else if (status === 'error') {
    el.classList.add('text-error');
    if (dot) dot.classList.add('bg-error');
    el.setAttribute('aria-label', 'System status: Core unreachable');
  }
}

const MetricsWidget = {
  setup() {
    const msgPerSec = ref(0);
    const activeSessions = ref(0);
    const roomCount = ref(0);
    const hasError = ref(false);
    let es = null;

    onMounted(() => {
      es = new EventSource('/admin/sse/metrics');

      es.addEventListener('metrics', (e) => {
        const data = JSON.parse(e.data);
        msgPerSec.value = data.msg_per_sec;
        activeSessions.value = data.active_sessions;
        roomCount.value = data.room_count;
        hasError.value = false;
        updateTopbar('ok');
      });

      es.addEventListener('error', (e) => {
        // SSE application-level error event from server
        hasError.value = true;
        updateTopbar('error');
      });

      es.onerror = () => {
        // Network-level EventSource error
        hasError.value = true;
        updateTopbar('error');
      };
    });

    onUnmounted(() => {
      if (es) es.close();
    });

    return { msgPerSec, activeSessions, roomCount, hasError };
  },
  template: `
    <div class="card-body py-6">
      <div v-if="hasError" role="alert" aria-live="assertive"
           class="badge badge-warning gap-2">
        Core unreachable
      </div>
      <dl v-else class="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div>
          <dt class="text-sm font-medium text-base-content/60">msg/s</dt>
          <dd class="mt-1 text-2xl font-mono">{{ msgPerSec.toFixed(1) }} msg/s</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-base-content/60">Active Sessions</dt>
          <dd class="mt-1 text-2xl font-mono">{{ activeSessions }}</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-base-content/60">Rooms</dt>
          <dd class="mt-1 text-2xl font-mono">{{ roomCount }}</dd>
        </div>
      </dl>
    </div>
  `
};

createApp(MetricsWidget).mount('#live-metrics');

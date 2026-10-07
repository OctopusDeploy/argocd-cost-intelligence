import React from 'react';
import {ArgoCDCostIntelligenceTab} from './plugin';
import { NamespaceDashboard } from './components/NamespaceDashboard';

// Types from Argo CD's ui/src/app/shared/services/extensions-service.ts (identical in 3.4 and 3.5).
// Argo CD types these callbacks loosely itself — any component, and the raw Application object —
// so mirroring its signatures needs `any`.
/* eslint-disable @typescript-eslint/no-explicit-any */
declare global {
    interface Window {
        extensionsAPI: {
            // registerAppViewExtension(component, title, icon, shouldDisplay?)
            // component is typed as AppViewExtensionComponent
            // (React.ComponentType<{application, tree}>), but any is safe for our usage.
            registerAppViewExtension(
                component: React.ComponentType<any>,
                title: string,
                icon: string,
                shouldDisplay?: (app: any) => boolean
            ): void;
            // registerSystemLevelExtension(component, title, path, icon)
            // Unchanged since v2.x. onAddSystemLevelExtension in app.tsx builds
            // iconClassName as `fa ${icon}`, so pass the bare FA class (e.g. 'fa-dollar-sign').
            registerSystemLevelExtension(
                component: React.ComponentType<any>,
                title: string,
                path: string,
                icon: string
            ): void;
        };
    }
}
/* eslint-enable @typescript-eslint/no-explicit-any */

((w: Window & typeof globalThis) => {
    // registerAppViewExtension is safe to call synchronously: application-details.tsx
    // calls services.extensions.getAppViewExtensions() on mount (reads the array),
    // so the extension is picked up even if this fires before the component mounts.
    w.extensionsAPI.registerAppViewExtension(
        ArgoCDCostIntelligenceTab,
        'Cost Intelligence',
        'fa fa-dollar-sign',
        () => true
    );

    // registerSystemLevelExtension MUST be deferred with setTimeout(0).
    //
    // Argo CD's app.tsx (3.4 and 3.5) registers the 'systemLevel' event listener only in the
    // App class constructor:
    //   services.extensions.addEventListener('systemLevel', this.onAddSystemLevelExtension.bind(this));
    //
    // There is NO fallback array-read (unlike app-view extensions which call
    // getAppViewExtensions() on mount). If the event fires before the App constructor
    // runs, the sidebar entry is permanently lost.
    //
    // Argo CD 3.5 renders with React 19's createRoot().render(), which schedules the initial
    // render via React's internal scheduler. This extensions.js script executes synchronously
    // as the next deferred <script> after the main bundle — before React's scheduler has
    // flushed the initial render and run the App constructor. Wrapping in setTimeout(0)
    // pushes past the current task and all pending microtasks, guaranteeing the App
    // has mounted and the listener is registered before the event fires. (Argo CD 3.4 uses
    // React 16's synchronous ReactDOM.render(), so there the App already exists and the
    // deferral is merely harmless.)
    setTimeout(() => {
        w.extensionsAPI.registerSystemLevelExtension(
            NamespaceDashboard,
            'Cost Intelligence',
            '/cost-intelligence',
            'fa-dollar-sign'
        );
    }, 0);
})(window);

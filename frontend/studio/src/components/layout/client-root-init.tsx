import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
import { App } from "antd";
import { useTranslation } from "react-i18next";

import { fetchChannelModels } from "@/services/api/image";
import { setImageRelayToken } from "@/services/api/image-relay";
import { createModelChannel, DEFAULT_SUB2API_BASE_URL, DEFAULT_SUB2API_GROUP_NAME, encodeChannelModel, guessCapability, modelOptionsFromChannels, normalizeChannelModels, useConfigStore, type ModelChannel, type Sub2ApiKeyOption } from "@/stores/use-config-store";
import { usePromptSourceScheduler } from "@/hooks/use-prompt-source-scheduler";

export function ClientRootInit({ children }: { children: ReactNode }) {
    const { message } = App.useApp();
    const { t } = useTranslation();
    const handledConfigParams = useRef(false);
    const importChannelCredentials = useConfigStore((state) => state.importChannelCredentials);
    const openConfigDialog = useConfigStore((state) => state.openConfigDialog);

    usePromptSourceScheduler();

    useEffect(() => {
        if (handledConfigParams.current) return;
        const searchParams = new URLSearchParams(window.location.search);
        const baseUrl = searchParams.get("baseUrl") || searchParams.get("baseurl");
        const apiKey = searchParams.get("apiKey") || searchParams.get("apikey");
        const sub2Token = baseUrl || apiKey ? null : localStorage.getItem("auth_token");
        if (!baseUrl && !apiKey && !sub2Token) return;
        handledConfigParams.current = true;
        searchParams.delete("baseUrl");
        searchParams.delete("baseurl");
        searchParams.delete("apiKey");
        searchParams.delete("apikey");
        searchParams.delete("token");
        if (sub2Token) {
            setImageRelayToken(sub2Token);
            void fetchSub2ApiKeys(sub2Token).then((keys) => {
                if (!keys.length) return;
                void bootstrapSub2Config(keys);
                message.success("已自动同步默认生图分组，其余分组可在新增配置中选择");
            });
            window.history.replaceState(null, "", `${window.location.pathname}${searchParams.size ? `?${searchParams}` : ""}${window.location.hash}`);
            return;
        }
        window.history.replaceState(null, "", `${window.location.pathname}${searchParams.size ? `?${searchParams}` : ""}${window.location.hash}`);
        const result = importChannelCredentials({ baseUrl, apiKey });
        openConfigDialog(false, "channels");
        if (result.status === "created") message.success(t("config.importedChannelCreated", { name: result.channelName }));
        else if (result.status === "updated") message.success(t("config.importedChannelUpdated", { name: result.channelName }));
        else if (result.status === "missing-base-url") message.error(t("config.importedChannelBaseUrlRequired"));
        else message.error(t("config.importedChannelBaseUrlInvalid"));
    }, [importChannelCredentials, message, openConfigDialog, t]);

    return <>{children}</>;
}

export async function bootstrapSub2Config(keys: Sub2ApiKey[]) {
    const store = useConfigStore.getState();
    useConfigStore.getState().setAvailableSub2Keys(keys);
    const defaultKey = keys.find(isDefaultImageKey);
    if (!defaultKey) return;
    const existingChannels = store.config.channels.filter((channel) => !channel.id.startsWith("sub2api-key:") && channel.id !== "default");
    const defaultChannel = createModelChannel({
        ...store.config.channels.find((channel) => channel.id === "default"),
        id: "default",
        name: DEFAULT_SUB2API_GROUP_NAME,
        baseUrl: DEFAULT_SUB2API_BASE_URL,
        apiKey: defaultKey.key,
        apiFormat: "openai",
        models: [],
    });
    useConfigStore.setState((state) => ({ config: { ...state.config, channels: [defaultChannel, ...existingChannels] } }));
    const loadedDefault = await loadSub2Models(defaultChannel);
    const channels = [loadedDefault, ...existingChannels];
    const options = modelOptionsFromChannels(channels);
    const imageModel = findModel([loadedDefault], "image");
    const videoModel = findModel([loadedDefault], "video");
    const textModel = findModel([loadedDefault], "text");
    const audioModel = findModel([loadedDefault], "audio");
    useConfigStore.setState((state) => ({
        config: {
            ...state.config,
            baseUrl: loadedDefault.baseUrl,
            apiKey: loadedDefault.apiKey,
            apiFormat: loadedDefault.apiFormat,
            channels,
            models: options,
            model: imageModel || textModel || state.config.model,
            imageModel: imageModel || state.config.imageModel,
            videoModel: videoModel || state.config.videoModel,
            textModel: textModel || state.config.textModel,
            audioModel: audioModel || state.config.audioModel,
        },
    }));
}

function isDefaultImageKey(item: Sub2ApiKeyOption) {
    const groupName = `${item.group?.name || item.group_name || item.name || ""}`.toLowerCase();
    return groupName.includes("生图") || groupName.includes("image2") || groupName.includes("image-2");
}

async function loadSub2Models(channel: ModelChannel) {
    try {
        const names = await fetchChannelModels(channel);
        return { ...channel, models: normalizeChannelModels(names).map((model) => ({ ...model, capability: guessCapability(model.name) })) };
    } catch {
        return channel;
    }
}

function findModel(channels: ModelChannel[], capability: "image" | "video" | "text" | "audio") {
    for (const channel of channels) {
        const model = channel.models.find((item) => item.capability === capability);
        if (model) return encodeChannelModel(channel.id, model.name);
    }
    return "";
}

type Sub2ApiKey = Sub2ApiKeyOption;

async function fetchSub2ApiKeys(token: string): Promise<Sub2ApiKey[]> {
    try {
        const response = await fetch("/api/v1/keys", { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
        if (!response.ok) return [];
        const payload = (await response.json()) as { data?: { items?: Sub2ApiKey[] } | Sub2ApiKey[]; items?: Sub2ApiKey[] };
        const items = Array.isArray(payload.data) ? payload.data : payload.data?.items || payload.items || [];
        return items.filter((item) => item?.key && item.status !== "disabled").map((item) => ({ ...item, key: item.key.trim() }));
    } catch {
        return [];
    }
}

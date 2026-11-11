import { describe, expect, it, vi } from "vitest";
import { dataUrlToBlob, dataUrlToFile } from "../image-utils";

vi.mock("@/i18n", () => ({ default: { t: (key: string) => key } }));

function readBytes(blob: Blob): Promise<number[]> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(Array.from(new Uint8Array(reader.result as ArrayBuffer)));
        reader.onerror = () => reject(reader.error);
        reader.readAsArrayBuffer(blob);
    });
}

describe("returned image data", () => {
    it("decodes binary bytes locally without fetch", async () => {
        const fetchSpy = vi.spyOn(globalThis, "fetch");
        try {
            const blob = dataUrlToBlob("data:image/png;base64,iVBORw0KGgo=");
            expect(blob.type).toBe("image/png");
            expect(await readBytes(blob)).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
            expect(fetchSpy).not.toHaveBeenCalled();
        } finally {
            fetchSpy.mockRestore();
        }
    });

    it("preserves the returned MIME type", () => {
        expect(dataUrlToBlob("data:image/webp;base64,AA==").type).toBe("image/webp");
        expect(dataUrlToBlob("data:image/svg+xml;charset=utf-8;base64,AA==").type).toBe("image/svg+xml;charset=utf-8");
    });

    it("accepts case-insensitive data scheme and base64 marker", async () => {
        expect(await readBytes(dataUrlToBlob("DATA:image/png;BASE64,AP8="))).toEqual([0, 255]);
    });

    it("retains the reference image name and exact original bytes", async () => {
        const file = dataUrlToFile({ id: "reference", name: "参考图.png", type: "image/png", dataUrl: "data:image/png;base64,AP8=" });
        expect(file.name).toBe("参考图.png");
        expect(file.type).toBe("image/png");
        expect(await readBytes(file)).toEqual([0, 255]);
    });

    it("uses the reference MIME type when the data URL omits it", () => {
        const file = dataUrlToFile({ id: "reference", name: "", type: "image/webp", dataUrl: "data:;base64,AA==" });
        expect(file.name).toBe("reference.png");
        expect(file.type).toBe("image/webp");
    });

    it.each(["", "https://images.invalid/a.png", "data:image/png,raw", "data:image/png;base64,", "data:image/png;base64,%%%"])("rejects invalid base64 input: %s", (value) => {
        expect(() => dataUrlToBlob(value)).toThrow();
    });
});

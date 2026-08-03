import { describe, expect, it } from "vitest";
import { appendRuntimeMessage } from "../features/workspace/AgentWorkspace";
import type { Message } from "../types";

describe("appendRuntimeMessage", () => {
  it("deduplicates a replayed message by stable id even when it is not adjacent", () => {
    const savedUser: Message = {
      id: "msg-user-1",
      role: "user",
      content: "hello",
      attachments: [{ id: "asset-1", file_type: "image", file_name: "shot.png" }]
    };
    const laterAssistant: Message = {
      id: "msg-assistant-1",
      role: "assistant",
      content: "partial response"
    };
    const replayedUser: Message = {
      id: "msg-user-1",
      role: "user",
      content: "hello",
      created_at: "2026-08-04T00:00:00Z"
    };

    const messages = appendRuntimeMessage([savedUser, laterAssistant], replayedUser);

    expect(messages).toHaveLength(2);
    expect(messages[0].created_at).toBe(replayedUser.created_at);
    expect(messages[0].attachments).toEqual(savedUser.attachments);
  });

  it("merges optimistic attachment messages with the saved user message", () => {
    const optimistic: Message = {
      role: "user",
      content: "能看到这张图片吗",
      attachments: [{ id: "asset-1", file_type: "image", mime_type: "image/png", file_name: "shot.png" }],
      created_at: "2026-07-06T11:28:01Z"
    };
    const saved: Message = {
      role: "user",
      content: "能看到这张图片吗\n\nAttached files: shot.png",
      message_index: 7,
      created_at: "2026-07-06T11:28:10Z"
    };

    const messages = appendRuntimeMessage([optimistic], saved);

    expect(messages).toHaveLength(1);
    expect(messages[0].message_index).toBe(7);
    expect(messages[0].content).toBe(saved.content);
    expect(messages[0].attachments).toEqual(optimistic.attachments);
  });

  it("does not merge unrelated user messages with attachments", () => {
    const previous: Message = {
      role: "user",
      content: "第一张图片",
      attachments: [{ id: "asset-1", file_type: "image", mime_type: "image/png", file_name: "one.png" }]
    };
    const next: Message = {
      role: "user",
      content: "第二张图片\n\nAttached files: two.png"
    };

    expect(appendRuntimeMessage([previous], next)).toHaveLength(2);
  });
});

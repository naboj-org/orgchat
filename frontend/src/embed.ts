import { mount } from "svelte";
import "./ui.css";
import "./embed.css";
import EmbeddedChat from "./EmbeddedChat.svelte";

mount(EmbeddedChat, { target: document.getElementById("app")! });

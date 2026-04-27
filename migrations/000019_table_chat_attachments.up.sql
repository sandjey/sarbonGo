--
-- PostgreSQL database dump
--

\restrict JKfHVxUFmqgFdUT3JXPjkdcFRWdKzbfw1IESE82nA8qYEWMUMMZMbuahRgI0TdY

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: chat_attachments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_attachments (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    message_id uuid,
    conversation_id uuid NOT NULL,
    uploader_id uuid NOT NULL,
    kind character varying(20) NOT NULL,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    thumb_path character varying(1024),
    width integer,
    height integer,
    duration_ms integer,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    media_file_id uuid,
    thumb_media_file_id uuid
);


--
-- Name: chat_attachments chat_attachments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_pkey PRIMARY KEY (id);


--
-- Name: idx_chat_attachments_conv_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_conv_created ON public.chat_attachments USING btree (conversation_id, created_at DESC);


--
-- Name: idx_chat_attachments_media_file; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_media_file ON public.chat_attachments USING btree (media_file_id);


--
-- Name: idx_chat_attachments_message_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_attachments_message_id ON public.chat_attachments USING btree (message_id);


--
-- Name: chat_attachments chat_attachments_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- Name: chat_attachments chat_attachments_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE RESTRICT;


--
-- Name: chat_attachments chat_attachments_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.chat_messages(id) ON DELETE SET NULL;


--
-- Name: chat_attachments chat_attachments_thumb_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_attachments
    ADD CONSTRAINT chat_attachments_thumb_media_file_id_fkey FOREIGN KEY (thumb_media_file_id) REFERENCES public.media_files(id) ON DELETE RESTRICT;


--
-- PostgreSQL database dump complete
--

\unrestrict JKfHVxUFmqgFdUT3JXPjkdcFRWdKzbfw1IESE82nA8qYEWMUMMZMbuahRgI0TdY


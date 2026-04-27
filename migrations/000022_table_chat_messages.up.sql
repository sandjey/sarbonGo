--
-- PostgreSQL database dump
--


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
-- Name: chat_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_messages (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    conversation_id uuid NOT NULL,
    sender_id uuid NOT NULL,
    body text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    type character varying(20) DEFAULT 'TEXT'::character varying NOT NULL,
    payload jsonb,
    delivered_at timestamp with time zone
);


--
-- Name: chat_messages chat_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_pkey PRIMARY KEY (id);


--
-- Name: idx_chat_messages_conversation_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_conversation_created ON public.chat_messages USING btree (conversation_id, created_at DESC);


--
-- Name: idx_chat_messages_deleted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_deleted ON public.chat_messages USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_chat_messages_sender; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_sender ON public.chat_messages USING btree (sender_id);


--
-- Name: idx_chat_messages_undelivered; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_messages_undelivered ON public.chat_messages USING btree (conversation_id) WHERE ((delivered_at IS NULL) AND (deleted_at IS NULL));


--
-- Name: chat_messages chat_messages_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_messages
    ADD CONSTRAINT chat_messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--



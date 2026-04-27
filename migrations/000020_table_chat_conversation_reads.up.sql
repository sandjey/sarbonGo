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
-- Name: chat_conversation_reads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_conversation_reads (
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_read_at timestamp with time zone DEFAULT '1970-01-01 06:00:00+06'::timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: chat_conversation_reads chat_conversation_reads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversation_reads
    ADD CONSTRAINT chat_conversation_reads_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: idx_chat_conversation_reads_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversation_reads_user ON public.chat_conversation_reads USING btree (user_id);


--
-- Name: chat_conversation_reads chat_conversation_reads_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversation_reads
    ADD CONSTRAINT chat_conversation_reads_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--



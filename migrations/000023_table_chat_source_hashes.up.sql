--
-- PostgreSQL database dump
--

\restrict 0PIfi9WgrJc4iYfC2DULRK6w44vZPISeUpvRQcDHhJGr23cig4DiKNJx4Qk48vD

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
-- Name: chat_source_hashes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_source_hashes (
    source_hash text NOT NULL,
    media_file_id uuid NOT NULL,
    thumb_media_file_id uuid,
    kind text NOT NULL,
    mime text NOT NULL,
    size_bytes bigint NOT NULL,
    duration_ms integer,
    width integer,
    height integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: chat_source_hashes chat_source_hashes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_pkey PRIMARY KEY (source_hash);


--
-- Name: idx_chat_source_hashes_media; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_source_hashes_media ON public.chat_source_hashes USING btree (media_file_id);


--
-- Name: idx_chat_source_hashes_thumb; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_source_hashes_thumb ON public.chat_source_hashes USING btree (thumb_media_file_id);


--
-- Name: chat_source_hashes chat_source_hashes_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE CASCADE;


--
-- Name: chat_source_hashes chat_source_hashes_thumb_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_source_hashes
    ADD CONSTRAINT chat_source_hashes_thumb_media_file_id_fkey FOREIGN KEY (thumb_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--

\unrestrict 0PIfi9WgrJc4iYfC2DULRK6w44vZPISeUpvRQcDHhJGr23cig4DiKNJx4Qk48vD

